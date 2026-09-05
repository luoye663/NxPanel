package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const defaultMemoryLimitPages = 1024 // 64 MiB

const MaxRPCBytes = 1 << 20

var (
	ErrInvokeUnsupported = errors.New("plugin does not export the nxpanel invoke ABI")
	ErrRPCTooLarge       = errors.New("plugin RPC payload exceeds 1 MiB")
)

// CapabilityBroker is the sole path from a sandboxed plugin into panel services.
// Implementations must authorize every method against the installation's approved
// permissions; the WASM runtime itself never exposes WASI, files, sockets or env.
type CapabilityBroker interface {
	Call(ctx context.Context, pluginID, method string, payload json.RawMessage) (json.RawMessage, error)
}

type denyBroker struct{}

func (denyBroker) Call(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("host capability is not available")
}

type Runtime interface {
	Validate(ctx context.Context, manifest *Manifest, modulePath string) error
	Enable(ctx context.Context, manifest *Manifest, modulePath string) error
	Disable(ctx context.Context, pluginID string) error
	Close(ctx context.Context) error
}

type WASMRuntime struct {
	runtime  wazero.Runtime
	mu       sync.Mutex
	invokeMu sync.Mutex
	modules  map[string]api.Module
	broker   CapabilityBroker
}

func NewWASMRuntime(ctx context.Context) *WASMRuntime {
	return NewWASMRuntimeWithBroker(ctx, denyBroker{})
}

func NewWASMRuntimeWithBroker(ctx context.Context, broker CapabilityBroker) *WASMRuntime {
	if broker == nil {
		broker = denyBroker{}
	}
	config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(defaultMemoryLimitPages)
	r := &WASMRuntime{runtime: wazero.NewRuntimeWithConfig(ctx, config), modules: make(map[string]api.Module), broker: broker}
	_, _ = r.runtime.NewHostModuleBuilder("nxpanel_v1").
		NewFunctionBuilder().WithFunc(r.hostCall).Export("host_call").
		Instantiate(ctx)
	return r
}

func (r *WASMRuntime) Validate(ctx context.Context, _ *Manifest, modulePath string) error {
	// Inspection executes untrusted start/health functions before permission
	// approval. Never share installed modules or their capability broker.
	isolated := NewWASMRuntime(ctx)
	defer isolated.Close(context.Background())
	wasm, err := os.ReadFile(modulePath)
	if err != nil {
		return err
	}
	compiled, err := isolated.runtime.CompileModule(ctx, wasm)
	if err != nil {
		return fmt.Errorf("compile WASM: %w", err)
	}
	defer compiled.Close(ctx)
	module, err := isolated.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return fmt.Errorf("instantiate WASM: %w", err)
	}
	defer module.Close(ctx)
	return callHealth(ctx, module)
}

func (r *WASMRuntime) Enable(ctx context.Context, manifest *Manifest, modulePath string) error {
	wasm, err := os.ReadFile(modulePath)
	if err != nil {
		return err
	}
	compiled, err := r.runtime.CompileModule(ctx, wasm)
	if err != nil {
		return fmt.Errorf("compile WASM: %w", err)
	}
	defer compiled.Close(ctx)
	module, err := r.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(manifest.ID))
	if err != nil {
		return fmt.Errorf("instantiate WASM: %w", err)
	}
	if err := callHealth(ctx, module); err != nil {
		_ = module.Close(ctx)
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.modules[manifest.ID]; exists {
		_ = module.Close(ctx)
		return errors.New("plugin is already enabled")
	}
	r.modules[manifest.ID] = module
	return nil
}

func callHealth(ctx context.Context, module api.Module) error {
	health := module.ExportedFunction("nxp_health")
	if health == nil {
		return errors.New("WASM does not export nxp_health")
	}
	results, err := health.Call(ctx)
	if err != nil {
		return fmt.Errorf("plugin health call failed: %w", err)
	}
	if len(results) != 1 || uint32(results[0]) != 0 {
		return errors.New("plugin health check returned unhealthy")
	}
	return nil
}

func (r *WASMRuntime) Disable(ctx context.Context, pluginID string) error {
	r.mu.Lock()
	module := r.modules[pluginID]
	delete(r.modules, pluginID)
	r.mu.Unlock()
	if module == nil {
		return nil
	}
	return module.Close(ctx)
}

// Invoke calls the stable Plugin API v1 JSON ABI. nxp_alloc reserves guest
// memory and nxp_invoke returns (pointer << 32 | length). Both request and
// response are bounded; any trap closes and removes the instance.
func (r *WASMRuntime) Invoke(ctx context.Context, pluginID, method string, payload []byte) ([]byte, error) {
	if len(method) == 0 || len(method) > 256 || len(payload) > MaxRPCBytes {
		return nil, ErrRPCTooLarge
	}
	r.invokeMu.Lock()
	defer r.invokeMu.Unlock()
	r.mu.Lock()
	module := r.modules[pluginID]
	r.mu.Unlock()
	if module == nil {
		return nil, errors.New("plugin is not enabled")
	}
	alloc, invoke := module.ExportedFunction("nxp_alloc"), module.ExportedFunction("nxp_invoke")
	if alloc == nil || invoke == nil {
		return nil, ErrInvokeUnsupported
	}
	methodPtr, err := guestAllocAndWrite(ctx, module, alloc, []byte(method))
	if err != nil {
		r.dropBroken(ctx, pluginID, module)
		return nil, err
	}
	payloadPtr, err := guestAllocAndWrite(ctx, module, alloc, payload)
	if err != nil {
		r.dropBroken(ctx, pluginID, module)
		return nil, err
	}
	result, err := invoke.Call(ctx, uint64(methodPtr), uint64(len(method)), uint64(payloadPtr), uint64(len(payload)))
	if err != nil || len(result) != 1 {
		r.dropBroken(ctx, pluginID, module)
		if err == nil {
			err = errors.New("nxp_invoke returned an invalid result")
		}
		return nil, fmt.Errorf("plugin invoke failed: %w", err)
	}
	ptr, size := uint32(result[0]>>32), uint32(result[0])
	if size > MaxRPCBytes {
		r.dropBroken(ctx, pluginID, module)
		return nil, ErrRPCTooLarge
	}
	b, ok := module.Memory().Read(ptr, size)
	if !ok {
		r.dropBroken(ctx, pluginID, module)
		return nil, errors.New("plugin returned an out-of-bounds buffer")
	}
	return append([]byte(nil), b...), nil
}

func guestAllocAndWrite(ctx context.Context, module api.Module, alloc api.Function, value []byte) (uint32, error) {
	result, err := alloc.Call(ctx, uint64(len(value)))
	if err != nil {
		return 0, fmt.Errorf("plugin allocation failed: %w", err)
	}
	if len(result) != 1 {
		return 0, errors.New("plugin allocation returned an invalid result")
	}
	ptr := uint32(result[0])
	if len(value) > 0 && !module.Memory().Write(ptr, value) {
		return 0, errors.New("plugin allocation returned an out-of-bounds buffer")
	}
	return ptr, nil
}

func (r *WASMRuntime) dropBroken(ctx context.Context, pluginID string, module api.Module) {
	r.mu.Lock()
	if r.modules[pluginID] == module {
		delete(r.modules, pluginID)
	}
	r.mu.Unlock()
	_ = module.Close(ctx)
}

// host_call ABI:
// (method_ptr, method_len, payload_ptr, payload_len, output_ptr, output_cap) -> i64
// The high 32 bits are status (0 success, 1 output buffer too small, 2 denied/error)
// and the low 32 bits are bytes written or required. Errors are returned as JSON.
func (r *WASMRuntime) hostCall(ctx context.Context, module api.Module, methodPtr, methodLen, payloadPtr, payloadLen, outputPtr, outputCap uint32) uint64 {
	if methodLen == 0 || methodLen > 256 || payloadLen > MaxRPCBytes || outputCap > MaxRPCBytes {
		return uint64(2) << 32
	}
	method, okMethod := module.Memory().Read(methodPtr, methodLen)
	payload, okPayload := module.Memory().Read(payloadPtr, payloadLen)
	if !okMethod || !okPayload {
		return uint64(2) << 32
	}
	response, err := r.broker.Call(ctx, module.Name(), string(method), append(json.RawMessage(nil), payload...))
	status := uint32(0)
	if err != nil {
		status = 2
		response, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	if len(response) > MaxRPCBytes {
		return uint64(2) << 32
	}
	if uint32(len(response)) > outputCap {
		return uint64(1)<<32 | uint64(uint32(len(response)))
	}
	if len(response) > 0 && !module.Memory().Write(outputPtr, response) {
		return uint64(2) << 32
	}
	return uint64(status)<<32 | uint64(uint32(len(response)))
}

func (r *WASMRuntime) Close(ctx context.Context) error { return r.runtime.Close(ctx) }
