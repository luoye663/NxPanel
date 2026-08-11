package upstream

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type store interface {
	List(context.Context) ([]*repo.NginxUpstream, error)
	GetByID(context.Context, string) (*repo.NginxUpstream, error)
	GetByName(context.Context, string) (*repo.NginxUpstream, error)
	Create(context.Context, *repo.NginxUpstream) error
	Update(context.Context, *repo.NginxUpstream) error
	Delete(context.Context, string) error
	CountReferences(context.Context, string) (int, error)
	GetAppliedState(context.Context) (*repo.NginxUpstreamAppliedState, error)
	RecordApplied(context.Context, string, string, string, string, []*repo.Backup) error
}

type operationStore interface {
	Create(*repo.Operation) error
	UpdateStatus(string, string) error
	UpdateError(string, string, string, string, string) error
}

type transactionAgent interface {
	ApplyTransaction(context.Context, *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
}

type Service struct {
	store      store
	operations operationStore
	agent      transactionAgent
	filePath   string
	mu         sync.Mutex
}

func NewService(store store, operations operationStore, agent transactionAgent, panelDir string) *Service {
	return &Service{
		store: store, operations: operations, agent: agent,
		filePath: filepath.Join(panelDir, "conf.d", "nxpanel-upstreams.conf"),
	}
}

func (s *Service) List(ctx context.Context) ([]*Upstream, error) {
	items, err := s.store.List(ctx)
	if err != nil {
		return nil, internalError(err)
	}
	result := make([]*Upstream, 0, len(items))
	for _, item := range items {
		result = append(result, toDTO(item))
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Upstream, error) {
	item, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, internalError(err)
	}
	if item == nil {
		return nil, app.NewAppError(app.ErrNotFound, "upstream not found", nil)
	}
	return toDTO(item), nil
}

func (s *Service) Status(ctx context.Context) (*StatusResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.store.List(ctx)
	if err != nil {
		return nil, internalError(err)
	}
	state, err := s.store.GetAppliedState(ctx)
	if err != nil {
		return nil, internalError(err)
	}
	desiredHash := hashContent(RenderFile(items))
	return &StatusResult{
		DesiredHash: desiredHash,
		AppliedHash: state.AppliedHash,
		AppliedAt:   state.AppliedAt,
		Synced:      state.AppliedHash != "" && state.AppliedHash == desiredHash && state.AppliedPath == s.filePath,
		Path:        s.filePath,
	}, nil
}

func (s *Service) Validate(ctx context.Context, req *SaveRequest) (*ValidateResult, error) {
	copyReq := *req
	copyReq.Servers = append([]ServerRequest(nil), req.Servers...)
	if err := NormalizeAndValidate(&copyReq); err != nil {
		return nil, validationError(err)
	}
	if copyReq.Name == "" {
		return nil, validationError(fmt.Errorf("name is required"))
	}
	candidate := requestToModel(&copyReq, "preview", time.Now().UTC().Format(time.RFC3339))
	items, err := s.store.List(ctx)
	if err != nil {
		return nil, internalError(err)
	}
	previewItems := make([]*repo.NginxUpstream, 0, len(items)+1)
	for _, item := range items {
		if !strings.EqualFold(item.Name, candidate.Name) {
			previewItems = append(previewItems, item)
		}
	}
	previewItems = append(previewItems, candidate)
	return &ValidateResult{RenderedBlock: RenderBlock(candidate), Preview: RenderFile(previewItems)}, nil
}

func (s *Service) Create(ctx context.Context, req *SaveRequest, requestID string) (*WriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := NormalizeAndValidate(req); err != nil {
		return nil, validationError(err)
	}
	if req.Name == "" {
		return nil, validationError(fmt.Errorf("name is required"))
	}
	if existing, err := s.store.GetByName(ctx, req.Name); err != nil {
		return nil, internalError(err)
	} else if existing != nil {
		return nil, app.NewAppError(app.ErrConflict, "upstream name already exists", nil)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	candidate := requestToModel(req, app.NewID("upstream"), now)
	opID, err := s.beginOperation(requestID, "nginx.upstream.create", candidate.ID, "create upstream "+candidate.Name)
	if err != nil {
		return nil, err
	}
	if err := s.store.Create(ctx, candidate); err != nil {
		return nil, s.desiredPersistenceError(opID, err)
	}
	if err := s.applyCurrent(ctx, opID); err != nil {
		return nil, s.desiredApplyError(opID, err)
	}
	if err := s.completeSuccess(opID); err != nil {
		return nil, err
	}
	return &WriteResult{Upstream: toDTO(candidate), OperationID: opID}, nil
}

func (s *Service) Update(ctx context.Context, id string, req *SaveRequest, requestID string) (*WriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, internalError(err)
	}
	if old == nil {
		return nil, app.NewAppError(app.ErrNotFound, "upstream not found", nil)
	}
	if req.Name != "" && req.Name != old.Name {
		return nil, validationError(fmt.Errorf("name cannot be changed"))
	}
	req.Name = old.Name
	if err := NormalizeAndValidate(req); err != nil {
		return nil, validationError(err)
	}
	candidate := requestToModel(req, id, time.Now().UTC().Format(time.RFC3339))
	candidate.CreatedAt = old.CreatedAt
	opID, err := s.beginOperation(requestID, "nginx.upstream.update", id, "update upstream "+old.Name)
	if err != nil {
		return nil, err
	}
	if err := s.store.Update(ctx, candidate); err != nil {
		return nil, s.desiredPersistenceError(opID, err)
	}
	if err := s.applyCurrent(ctx, opID); err != nil {
		return nil, s.desiredApplyError(opID, err)
	}
	if err := s.completeSuccess(opID); err != nil {
		return nil, err
	}
	return &WriteResult{Upstream: toDTO(candidate), OperationID: opID}, nil
}

func (s *Service) Delete(ctx context.Context, id, requestID string) (*WriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, internalError(err)
	}
	if old == nil {
		return nil, app.NewAppError(app.ErrNotFound, "upstream not found", nil)
	}
	refs, err := s.store.CountReferences(ctx, id)
	if err != nil {
		return nil, internalError(err)
	}
	if refs > 0 {
		return nil, app.NewAppError(app.ErrConflict, "upstream is still referenced", map[string]any{"reference_count": refs})
	}
	opID, err := s.beginOperation(requestID, "nginx.upstream.delete", id, "delete upstream "+old.Name)
	if err != nil {
		return nil, err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		if isForeignKeyConstraint(err) {
			return nil, s.deleteConflict(ctx, opID, id)
		}
		return nil, s.desiredPersistenceError(opID, err)
	}
	if err := s.applyCurrent(ctx, opID); err != nil {
		return nil, s.desiredApplyError(opID, err)
	}
	if err := s.completeSuccess(opID); err != nil {
		return nil, err
	}
	return &WriteResult{OperationID: opID}, nil
}

func (s *Service) deleteConflict(ctx context.Context, operationID, id string) error {
	refs, _ := s.store.CountReferences(ctx, id)
	cause := fmt.Errorf("upstream is still referenced")
	if terminalErr := s.operations.UpdateError(operationID, "failed", app.ErrConflict, cause.Error(), ""); terminalErr != nil {
		return operationTerminalError(operationID, cause, terminalErr, false, "not_attempted", false)
	}
	return app.NewAppError(app.ErrConflict, cause.Error(), map[string]any{"operation_id": operationID, "reference_count": refs})
}

func isForeignKeyConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "foreign key constraint")
}

func (s *Service) Sync(ctx context.Context, requestID string) (*SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	opID, err := s.beginOperation(requestID, "nginx.upstream.sync", "nginx_upstreams", "sync global upstreams")
	if err != nil {
		return nil, err
	}
	if err := s.applyCurrent(ctx, opID); err != nil {
		return nil, s.desiredApplyError(opID, err)
	}
	if err := s.completeSuccess(opID); err != nil {
		return nil, err
	}
	return &SyncResult{OperationID: opID, Path: s.filePath}, nil
}

type applyFailure struct {
	cause   error
	code    string
	outcome string
}

func (e *applyFailure) Error() string { return e.cause.Error() }

func (s *Service) applyCurrent(ctx context.Context, operationID string) error {
	items, err := s.store.List(ctx)
	if err != nil {
		return &applyFailure{cause: err, code: app.ErrInternalError, outcome: "not_attempted"}
	}
	content := RenderFile(items)
	desiredHash := hashContent(content)
	result, err := s.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: operationID,
		Changes: []agentclient.FileChangeRequest{
			{Type: "mkdir", Path: filepath.Dir(s.filePath), Perm: 0755},
			{Type: "write", Path: s.filePath, ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)), Perm: 0644},
		},
		TestNginx: true, ReloadNginx: true,
	})
	if err != nil {
		return &applyFailure{cause: err, code: app.ErrAgentUnavailable, outcome: "unknown"}
	}
	if result == nil {
		return &applyFailure{cause: fmt.Errorf("agent returned an empty transaction response"), code: app.ErrInternalError, outcome: "applied"}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	backups := make([]*repo.Backup, 0, len(result.Backups))
	for _, item := range result.Backups {
		backups = append(backups, &repo.Backup{
			ID: app.NewID("backup"), OperationID: operationID, FilePath: item.FilePath,
			BackupPath: item.BackupPath, FileExisted: item.Existed, CreatedAt: now,
		})
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.RecordApplied(recordCtx, operationID, desiredHash, s.filePath, now, backups); err != nil {
		return &applyFailure{cause: fmt.Errorf("record applied upstream state: %w", err), code: app.ErrInternalError, outcome: "applied"}
	}
	return nil
}

func (s *Service) beginOperation(requestID, action, targetID, message string) (string, error) {
	opID := app.NewOperationID()
	err := s.operations.Create(&repo.Operation{
		ID: opID, Action: action, TargetType: "nginx_upstream", TargetID: targetID,
		Status: "pending", RequestID: requestID, Actor: "admin", Message: message,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", internalError(fmt.Errorf("create operation: %w", err))
	}
	return opID, nil
}

func (s *Service) desiredPersistenceError(operationID string, cause error) error {
	if terminalErr := s.operations.UpdateError(operationID, "failed", app.ErrInternalError, cause.Error(), ""); terminalErr != nil {
		return operationTerminalError(operationID, cause, terminalErr, false, "not_attempted", false)
	}
	return app.NewAppError(app.ErrInternalError, cause.Error(), map[string]any{
		"operation_id":  operationID,
		"desired_saved": false,
	})
}

func (s *Service) desiredApplyError(operationID string, err error) error {
	failure, ok := err.(*applyFailure)
	if !ok {
		failure = &applyFailure{cause: err, code: app.ErrInternalError, outcome: "unknown"}
	}
	if terminalErr := s.operations.UpdateError(operationID, "failed", failure.code, failure.cause.Error(), ""); terminalErr != nil {
		return operationTerminalError(operationID, failure.cause, terminalErr, true, failure.outcome, true)
	}
	details := map[string]any{
		"operation_id":  operationID,
		"desired_saved": true,
		"sync_required": true,
	}
	setApplyOutcomeDetails(details, failure.outcome)
	return app.NewAppError(failure.code, "failed to apply upstream configuration: "+failure.cause.Error(), details)
}

func (s *Service) completeSuccess(operationID string) error {
	if err := s.operations.UpdateStatus(operationID, "success"); err != nil {
		return operationTerminalError(operationID, fmt.Errorf("upstream configuration applied"), err, true, "applied", false)
	}
	return nil
}

func operationTerminalError(operationID string, cause, terminalErr error, desiredSaved bool, outcome string, syncRequired bool) error {
	details := map[string]any{
		"operation_id": operationID, "desired_saved": desiredSaved,
		"sync_required": syncRequired, "operation_update_failed": true,
	}
	setApplyOutcomeDetails(details, outcome)
	return app.NewAppError(app.ErrInternalError,
		fmt.Sprintf("%v; update operation terminal state: %v", cause, terminalErr),
		details)
}

func setApplyOutcomeDetails(details map[string]any, outcome string) {
	if outcome == "applied" {
		details["config_applied"] = true
		return
	}
	details["config_apply_outcome"] = outcome
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum)
}

func requestToModel(req *SaveRequest, id, now string) *repo.NginxUpstream {
	u := &repo.NginxUpstream{
		ID: id, Name: req.Name, Algorithm: req.Algorithm, HashKey: req.HashKey,
		Consistent: req.Consistent, Keepalive: req.Keepalive,
		KeepaliveRequests: req.KeepaliveRequests, KeepaliveTimeoutSeconds: req.KeepaliveTimeoutSeconds,
		AdvancedDirectives: req.AdvancedDirectives, CreatedAt: now, UpdatedAt: now,
		Servers: make([]*repo.NginxUpstreamServer, 0, len(req.Servers)),
	}
	for _, item := range req.Servers {
		u.Servers = append(u.Servers, &repo.NginxUpstreamServer{
			ID: app.NewID("upstream_server"), UpstreamID: id, Address: item.Address,
			Weight: item.Weight, MaxFails: item.MaxFails, FailTimeoutSeconds: item.FailTimeoutSeconds,
			Backup: item.Backup, Down: item.Down, SortOrder: item.SortOrder, CreatedAt: now, UpdatedAt: now,
		})
	}
	return u
}

func toDTO(item *repo.NginxUpstream) *Upstream {
	result := &Upstream{
		ID: item.ID, Name: item.Name, Algorithm: item.Algorithm, HashKey: item.HashKey,
		Consistent: item.Consistent, Keepalive: item.Keepalive,
		KeepaliveRequests: item.KeepaliveRequests, KeepaliveTimeoutSeconds: item.KeepaliveTimeoutSeconds,
		AdvancedDirectives: item.AdvancedDirectives, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		ReferenceCount: item.ReferenceCount,
		Servers:        make([]Server, 0, len(item.Servers)),
	}
	for _, server := range item.Servers {
		result.Servers = append(result.Servers, Server{
			ID: server.ID, Address: server.Address, Weight: server.Weight, MaxFails: server.MaxFails,
			FailTimeoutSeconds: server.FailTimeoutSeconds, Backup: server.Backup, Down: server.Down,
			SortOrder: server.SortOrder,
		})
	}
	return result
}

func validationError(err error) error {
	return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
}

func internalError(err error) error {
	if appErr, ok := err.(*app.AppError); ok {
		return appErr
	}
	if err == sql.ErrNoRows {
		return app.NewAppError(app.ErrNotFound, "upstream not found", nil)
	}
	return app.NewAppError(app.ErrInternalError, err.Error(), nil)
}
