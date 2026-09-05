package plugin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrPermissionApprovalRequired = errors.New("plugin permission approval required")
	ErrPluginIDMismatch           = errors.New("plugin package id/version does not match catalog")
	ErrPluginEnabled              = errors.New("plugin must be disabled before removal")
)

type InstallRequest struct {
	ID                  string   `json:"id"`
	Version             string   `json:"version,omitempty"`
	ApprovedPermissions []string `json:"approved_permissions"`
	Permissions         []string `json:"permissions,omitempty"`
}

type Service struct {
	repo           *Repository
	root           string
	catalog        Catalog
	runtime        Runtime
	developer      *DeveloperManager
	authorizations *AuthorizationManager
	official       *OfficialServiceClient
}

func NewService(db *sql.DB, dataDir string, catalog Catalog, runtime Runtime) *Service {
	official, _ := OfficialServiceConfig()
	return NewServiceWithOfficialClient(db, dataDir, catalog, runtime, official)
}

func NewServiceWithOfficialClient(db *sql.DB, dataDir string, catalog Catalog, runtime Runtime, official *OfficialServiceClient) *Service {
	if runtime == nil {
		runtime = NewWASMRuntimeWithBroker(context.Background(), NewCapabilityBroker(db))
	}
	root := filepath.Join(dataDir, "plugins")
	return &Service{repo: NewRepository(db), root: root, catalog: catalog, runtime: runtime, developer: newDeveloperManager(db, root, catalog, runtime), official: official, authorizations: NewAuthorizationManager(db, dataDir, official)}
}
func (s *Service) Close(ctx context.Context) error { return s.runtime.Close(ctx) }
func (s *Service) Catalog(ctx context.Context) ([]CatalogEntry, error) {
	if s.catalog == nil {
		return []CatalogEntry{}, nil
	}
	return s.catalog.List(ctx)
}
func (s *Service) RefreshCatalog(ctx context.Context) error {
	if s.catalog == nil {
		return errors.New("plugin catalog is not configured")
	}
	if refreshable, ok := s.catalog.(RefreshableCatalog); ok {
		if err := refreshable.Refresh(ctx); err != nil {
			return err
		}
	}
	entries, err := s.catalog.List(ctx)
	if err != nil {
		return err
	}
	ids := make(map[string]bool, len(entries))
	for _, entry := range entries {
		ids[entry.ID] = true
	}
	return s.repo.SyncDeveloperConflicts(ctx, ids)
}
func (s *Service) RepositoryStatus() RepositoryStatus {
	if provider, ok := s.catalog.(interface{ Status() RepositoryStatus }); ok {
		return provider.Status()
	}
	return RepositoryStatus{Configured: s.catalog != nil}
}
func (s *Service) List(ctx context.Context) ([]Installation, error) { return s.repo.List(ctx) }
func (s *Service) Get(ctx context.Context, id string) (*Installation, error) {
	return s.repo.Get(ctx, id)
}

// RestoreEnabled reconstructs the in-memory sandbox set after an API restart.
// Backends that no longer validate are disabled to keep persisted state honest.
func (s *Service) RestoreEnabled(ctx context.Context) error {
	items, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !item.Enabled || item.Manifest == nil {
			continue
		}
		modulePath := filepath.Join(s.root, item.ID, item.ActiveVersion, filepath.FromSlash(item.Manifest.Backend))
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.runtime.Enable(callCtx, item.Manifest, modulePath)
		cancel()
		if err != nil {
			_ = s.repo.SetEnabled(context.Background(), item.ID, false)
		}
	}
	return nil
}

func (s *Service) Install(ctx context.Context, req InstallRequest) (*Installation, error) {
	if s.catalog == nil {
		return nil, errors.New("plugin catalog is not configured")
	}
	entry, err := s.catalog.Resolve(ctx, req.ID, req.Version)
	if err != nil {
		return nil, err
	}
	if existing, getErr := s.repo.Get(ctx, req.ID); getErr == nil && existing.Source == "developer" {
		return nil, ErrOfficialIDReserved
	} else if getErr == nil && existing.Enabled {
		return nil, ErrPluginEnabled
	} else if getErr != nil && !errors.Is(getErr, ErrPluginNotFound) {
		return nil, getErr
	}
	if err := os.MkdirAll(filepath.Join(s.root, ".staging"), 0o750); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(filepath.Join(s.root, ".staging"), "install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	packagePath := filepath.Join(stage, "plugin.nxp")
	if err := s.fetchOfficialPackage(ctx, entry, packagePath); err != nil {
		return nil, err
	}
	unpacked := filepath.Join(stage, "unpacked")
	manifest, err := VerifyPackage(ctx, packagePath, entry.PackageSHA256, unpacked, DefaultPackageLimits)
	if err != nil {
		return nil, err
	}
	if manifest.ID != entry.ID || manifest.Version != entry.Version {
		return nil, ErrPluginIDMismatch
	}
	catalogPermissions := make([]string, 0, len(entry.Permissions))
	for _, permission := range entry.Permissions {
		catalogPermissions = append(catalogPermissions, permission.Name)
	}
	if !sameStrings(sortedUnique(catalogPermissions), sortedUnique(manifest.Permissions)) ||
		!sameStrings(sortedUnique(entry.Providers), sortedUnique(manifest.Providers)) {
		return nil, fmt.Errorf("%w: catalog permissions or providers do not match manifest", ErrPluginIDMismatch)
	}
	approved := req.ApprovedPermissions
	if len(approved) == 0 {
		approved = req.Permissions
	}
	approved = sortedUnique(approved)
	if !sameStrings(approved, manifest.Permissions) {
		return nil, fmt.Errorf("%w: requested=%v", ErrPermissionApprovalRequired, manifest.Permissions)
	}
	validateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = s.runtime.Validate(validateCtx, manifest, filepath.Join(unpacked, filepath.FromSlash(manifest.Backend)))
	cancel()
	if err != nil {
		return nil, err
	}
	finalPath := filepath.Join(s.root, manifest.ID, manifest.Version)
	if _, err := os.Stat(finalPath); err == nil {
		if existing, getErr := s.repo.Get(ctx, manifest.ID); getErr == nil && existing.ActiveVersion == manifest.Version {
			return existing, nil
		}
		return nil, errors.New("plugin version is already installed")
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
		return nil, err
	}
	if err := os.Rename(unpacked, finalPath); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(finalPath)
		}
	}()
	hash, err := fileSHA256(packagePath)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Activate(ctx, manifest, hash, finalPath, approved); err != nil {
		return nil, err
	}
	committed = true
	return s.repo.Get(ctx, manifest.ID)
}

func (s *Service) fetchOfficialPackage(ctx context.Context, entry CatalogEntry, destination string) error {
	if s.official == nil {
		return ErrRepositoryNotConfigured
	}
	instanceID, err := s.authorizations.InstanceID(ctx)
	if err != nil {
		return err
	}
	bearer := ""
	boundID := ""
	if entry.Access == CatalogAccessLicensed {
		var bindErr error
		boundID, bindErr = s.authorizations.BoundID(ctx, entry.ID)
		if bindErr != nil {
			return bindErr
		}
		if boundID != "" {
			bearer, err = s.authorizations.AccessToken(ctx, boundID)
		}
		if err != nil {
			if errors.Is(err, ErrReauthorizationRequired) {
				return s.authorizationRequired(ctx, entry)
			}
			return err
		}
	}
	resolved, err := s.official.ResolveDownload(ctx, DownloadResolveRequest{PluginID: entry.ID, Version: entry.Version, Target: entry.Package, InstanceUUID: instanceID, PanelVersion: panelVersion(), Runtime: "wasm-v1"}, bearer)
	if err != nil {
		var serviceErr *ServiceError
		if errors.As(err, &serviceErr) && (serviceErr.Code == "authorization_required" || serviceErr.Code == "token_expired") {
			if boundID != "" {
				_ = s.authorizations.MarkInvalid(ctx, boundID)
			}
			serviceErr.Code = "authorization_required"
			return s.authorizationRequired(ctx, entry)
		}
		return err
	}
	fetcher, ok := s.catalog.(interface {
		FetchResolved(context.Context, CatalogEntry, DownloadResolveResponse, string) error
	})
	if !ok {
		return errors.New("official catalog does not support authorized downloads")
	}
	return fetcher.FetchResolved(ctx, entry, resolved, destination)
}

func (s *Service) authorizationRequired(ctx context.Context, entry CatalogEntry) error {
	accounts, _ := s.authorizations.List(ctx)
	return &ServiceError{Status: 401, Code: "authorization_required", Message: "plugin authorization is required", Details: map[string]any{"plugin_id": entry.ID, "version": entry.Version, "authorizations": accounts}}
}

func panelVersion() string { return "nxpanel/" + strings.TrimSpace(OfficialPanelVersion) }

func (s *Service) Enable(ctx context.Context, id string) (*Installation, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.Enabled {
		return item, nil
	}
	if item.Source == "developer" {
		if err := s.developer.requireEnabled(ctx); err != nil {
			return nil, err
		}
		if item.SourceConflict {
			return nil, ErrOfficialIDReserved
		}
	}
	modulePath := filepath.Join(s.root, id, item.ActiveVersion, filepath.FromSlash(item.Manifest.Backend))
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = s.runtime.Enable(callCtx, item.Manifest, modulePath)
	cancel()
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetEnabled(ctx, id, true); err != nil {
		_ = s.runtime.Disable(context.Background(), id)
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) DeveloperMode(ctx context.Context) (bool, error) { return s.developer.Enabled(ctx) }
func (s *Service) SetDeveloperMode(ctx context.Context, enabled, acknowledged bool) error {
	return s.developer.SetEnabled(ctx, enabled, acknowledged)
}
func (s *Service) InspectDeveloperPackage(ctx context.Context, filename string, src io.Reader) (*DeveloperInspection, error) {
	return s.developer.Inspect(ctx, filename, src)
}
func (s *Service) DiscardDeveloperPackage(token string) { s.developer.discard(token) }
func (s *Service) InstallDeveloperPackage(ctx context.Context, token string, approved []string) (*Installation, error) {
	staged, err := s.developer.take(ctx, token)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staged.root)
	manifest := staged.inspection.Manifest
	if existing, getErr := s.repo.Get(ctx, manifest.ID); getErr == nil {
		if existing.Source != "developer" {
			return nil, ErrOfficialIDReserved
		}
		if existing.Enabled {
			return nil, ErrPluginEnabled
		}
	} else if !errors.Is(getErr, ErrPluginNotFound) {
		return nil, getErr
	}
	if err := s.developer.checkOfficialID(ctx, manifest.ID); err != nil {
		return nil, err
	}
	approved = sortedUnique(approved)
	if !sameStrings(approved, manifest.Permissions) {
		return nil, fmt.Errorf("%w: requested=%v", ErrPermissionApprovalRequired, manifest.Permissions)
	}
	hash, err := rehashPackage(staged.packagePath)
	if err != nil {
		return nil, err
	}
	if hash != staged.inspection.PackageSHA256 {
		return nil, errors.New("developer package changed after inspection")
	}
	finalPath := filepath.Join(s.root, manifest.ID, manifest.Version)
	if _, err := os.Stat(finalPath); err == nil {
		return nil, errors.New("plugin version is already installed")
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
		return nil, err
	}
	if err := os.Rename(staged.unpacked, finalPath); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(finalPath)
		}
	}()
	if err := s.repo.ActivateWithSource(ctx, manifest, hash, finalPath, approved, "developer", "developer_unverified"); err != nil {
		return nil, err
	}
	committed = true
	return s.repo.Get(ctx, manifest.ID)
}

func (s *Service) Authorizations(ctx context.Context) ([]AuthorizationSummary, error) {
	return s.authorizations.List(ctx)
}
func (s *Service) StartDeviceAuthorization(ctx context.Context, id, version string) (*DeviceAttempt, error) {
	if s.catalog == nil {
		return nil, ErrRepositoryNotConfigured
	}
	entry, err := s.catalog.Resolve(ctx, id, version)
	if err != nil {
		return nil, err
	}
	if entry.Access != CatalogAccessLicensed {
		return nil, errors.New("plugin does not require account authorization")
	}
	instance, err := s.authorizations.InstanceID(ctx)
	if err != nil {
		return nil, err
	}
	return s.authorizations.StartDevice(ctx, entry.ID, entry.Version, instance)
}
func (s *Service) PollDeviceAuthorization(ctx context.Context, attempt string) (DevicePollResult, error) {
	return s.authorizations.PollDevice(ctx, attempt)
}
func (s *Service) BindAuthorization(ctx context.Context, pluginID, version, authorizationID string) error {
	if s.catalog == nil || s.official == nil {
		return ErrRepositoryNotConfigured
	}
	entry, err := s.catalog.Resolve(ctx, pluginID, version)
	if err != nil {
		return err
	}
	if entry.Access != CatalogAccessLicensed {
		return errors.New("plugin does not require account authorization")
	}
	token, err := s.authorizations.AccessToken(ctx, authorizationID)
	if err != nil {
		if errors.Is(err, ErrReauthorizationRequired) {
			return s.authorizationRequired(ctx, entry)
		}
		return err
	}
	instance, err := s.authorizations.InstanceID(ctx)
	if err != nil {
		return err
	}
	resolved, err := s.official.ResolveDownload(ctx, DownloadResolveRequest{PluginID: entry.ID, Version: entry.Version, Target: entry.Package, InstanceUUID: instance, PanelVersion: panelVersion(), Runtime: "wasm-v1"}, token)
	if err != nil {
		var serviceErr *ServiceError
		if errors.As(err, &serviceErr) && (serviceErr.Code == "authorization_required" || serviceErr.Code == "token_expired") {
			_ = s.authorizations.MarkInvalid(ctx, authorizationID)
			return s.authorizationRequired(ctx, entry)
		}
		return err
	}
	if resolved.Target != entry.Package || resolved.SHA256 != entry.PackageSHA256 || resolved.Length != entry.Size || resolved.ExpiresAt.IsZero() || !resolved.ExpiresAt.After(time.Now().UTC()) {
		return ErrPluginIDMismatch
	}
	return s.authorizations.Bind(ctx, pluginID, authorizationID)
}
func (s *Service) RevokeAuthorization(ctx context.Context, id string) error {
	return s.authorizations.Revoke(ctx, id)
}

func (s *Service) Invoke(ctx context.Context, id, method string, payload []byte) ([]byte, error) {
	if len(payload) > MaxRPCBytes {
		return nil, ErrRPCTooLarge
	}
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !item.Enabled {
		return nil, errors.New("plugin is disabled")
	}
	invoker, ok := s.runtime.(interface {
		Invoke(context.Context, string, string, []byte) ([]byte, error)
	})
	if !ok {
		return nil, ErrInvokeUnsupported
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := invoker.Invoke(callCtx, id, method, payload)
	if err != nil {
		_ = s.repo.SetHealth(context.Background(), id, "unhealthy", err.Error())
		return nil, err
	}
	_ = s.repo.SetHealth(ctx, id, "healthy", "")
	return result, nil
}
func (s *Service) Disable(ctx context.Context, id string) (*Installation, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !item.Enabled {
		return item, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = s.runtime.Disable(callCtx, id)
	cancel()
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetEnabled(ctx, id, false); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}
func (s *Service) Delete(ctx context.Context, id string) error {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Enabled {
		return ErrPluginEnabled
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.root, id))
}
func (s *Service) Contributions(ctx context.Context) ([]Contribution, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Contribution, 0)
	for _, item := range items {
		if item.Enabled {
			for _, contribution := range item.Manifest.Contributions {
				contribution.ID = item.ID + ":" + contribution.ID
				contribution.PluginID = item.ID
				result = append(result, contribution)
			}
		}
	}
	return result, nil
}
func (s *Service) UIAsset(ctx context.Context, id, asset string) (string, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !item.Enabled || item.Manifest.UI == nil || (asset != item.Manifest.UI.Script && asset != item.Manifest.UI.Style) {
		return "", ErrPluginNotFound
	}
	if err := validatePackagePath(asset); err != nil {
		return "", ErrPluginNotFound
	}
	return filepath.Join(s.root, id, item.ActiveVersion, filepath.FromSlash(asset)), nil
}
func sortedUnique(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
