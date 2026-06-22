package scheduledtask

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type TaskHandler interface {
	Type() string
	Definition() TaskDefinition
	DefaultParams() json.RawMessage
	ValidateParams(raw json.RawMessage) (json.RawMessage, error)
	Run(ctx context.Context, task Task, run RunContext) error
}

type Registry struct {
	mu       sync.RWMutex
	handlers map[string]TaskHandler
	defs     []TaskDefinition
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]TaskHandler)}
}

func (r *Registry) Register(handler TaskHandler) error {
	if handler == nil || handler.Type() == "" {
		return fmt.Errorf("计划任务 handler 不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[handler.Type()]; exists {
		return fmt.Errorf("计划任务类型重复: %s", handler.Type())
	}
	r.handlers[handler.Type()] = handler
	r.defs = nil // 注册发生变化时清空定义缓存，避免列表接口重复分配。
	return nil
}

func (r *Registry) Get(taskType string) (TaskHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[taskType]
	return handler, ok
}

func (r *Registry) Definitions() []TaskDefinition {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defs == nil {
		r.defs = make([]TaskDefinition, 0, len(r.handlers))
		for _, handler := range r.handlers {
			r.defs = append(r.defs, handler.Definition())
		}
	}
	result := make([]TaskDefinition, len(r.defs))
	copy(result, r.defs)
	return result
}
