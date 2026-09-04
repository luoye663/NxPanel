package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	tufconfig "github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

const (
	CatalogTargetPath    = "catalog/v1/index.json"
	CatalogSchemaVersion = 1
)

var (
	ErrRepositoryNotConfigured = errors.New("official plugin repository is not configured")
	ErrLicenseRequired         = errors.New("plugin requires a license entitlement")
)

type CatalogAccess string

const (
	CatalogAccessFree     CatalogAccess = "free"
	CatalogAccessLicensed CatalogAccess = "licensed"
)

type CatalogEntry struct {
	ID            string                 `json:"id"`
	Version       string                 `json:"version"`
	Package       string                 `json:"package"`
	PackageSHA256 string                 `json:"package_sha256"`
	Size          int64                  `json:"size"`
	Name          string                 `json:"name"`
	Summary       string                 `json:"summary"`
	Description   string                 `json:"description,omitempty"`
	Publisher     string                 `json:"publisher"`
	Icon          string                 `json:"icon,omitempty"`
	Permissions   []PermissionDescriptor `json:"permissions,omitempty"`
	Providers     []string               `json:"required_providers,omitempty"`
	Access        CatalogAccess          `json:"access"`
}
type PermissionDescriptor struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Risk        string `json:"risk,omitempty"`
}

// CatalogIndex is the TUF-protected catalog/v1/index.json wire format.
type CatalogIndex struct {
	SchemaVersion int             `json:"schema_version"`
	Version       int64           `json:"version"`
	GeneratedAt   time.Time       `json:"generated_at"`
	Plugins       []CatalogPlugin `json:"plugins"`
}
type CatalogPlugin struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description,omitempty"`
	Publisher   string                 `json:"publisher"`
	Icon        string                 `json:"icon,omitempty"`
	Access      CatalogAccess          `json:"access"`
	Permissions []PermissionDescriptor `json:"permissions,omitempty"`
	Providers   []string               `json:"required_providers,omitempty"`
	Versions    []CatalogVersion       `json:"versions"`
}
type CatalogVersion struct {
	Version          string   `json:"version"`
	Target           string   `json:"target"`
	SHA256           string   `json:"sha256"`
	Length           int64    `json:"length"`
	PanelVersion     string   `json:"panel_version,omitempty"`
	PluginAPIVersion string   `json:"plugin_api_version"`
	Permissions      []string `json:"permissions,omitempty"`
	Providers        []string `json:"providers,omitempty"`
}

type Catalog interface {
	List(context.Context) ([]CatalogEntry, error)
	Resolve(context.Context, string, string) (CatalogEntry, error)
	Fetch(context.Context, CatalogEntry, string) error
}
type RefreshableCatalog interface{ Refresh(context.Context) error }
type RepositoryStatus struct {
	Configured       bool      `json:"configured"`
	UsingCache       bool      `json:"using_cache"`
	CatalogVersion   int64     `json:"catalog_version,omitempty"`
	LastRefreshAt    time.Time `json:"last_refresh_at,omitempty"`
	LastRefreshError string    `json:"last_refresh_error,omitempty"`
}
type RepositoryConfig struct {
	MetadataURL   string
	TargetsURL    string
	BootstrapRoot []byte
	CacheDir      string
	HTTPClient    *http.Client
	AllowHTTP     bool
}

// TUFRepository uses go-tuf's complete root -> timestamp -> snapshot -> targets
// workflow. Calls are serialized because Updater is deliberately not thread-safe.
type TUFRepository struct {
	cfg    RepositoryConfig
	mu     sync.Mutex
	status RepositoryStatus
	index  *CatalogIndex
}

func OpenTUFRepository(cfg RepositoryConfig) (*TUFRepository, error) {
	if strings.TrimSpace(cfg.MetadataURL) == "" || strings.TrimSpace(cfg.TargetsURL) == "" || len(cfg.BootstrapRoot) == 0 {
		return nil, ErrRepositoryNotConfigured
	}
	for _, raw := range []string{cfg.MetadataURL, cfg.TargetsURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && !(cfg.AllowHTTP && u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
			return nil, fmt.Errorf("official plugin repository URL must use HTTPS: %q", raw)
		}
	}
	if _, err := trustedmetadata.New(cfg.BootstrapRoot); err != nil {
		return nil, fmt.Errorf("invalid bootstrap root: %w", err)
	}
	if cfg.CacheDir == "" {
		return nil, errors.New("plugin repository cache directory is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	cfg.HTTPClient = secureHTTPClient(cfg.HTTPClient, cfg.AllowHTTP)
	for _, dir := range []string{cfg.CacheDir, filepath.Join(cfg.CacheDir, "metadata"), filepath.Join(cfg.CacheDir, "targets"), filepath.Join(cfg.CacheDir, "roots")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return &TUFRepository{cfg: cfg, status: RepositoryStatus{Configured: true}}, nil
}
func (r *TUFRepository) Status() RepositoryStatus { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (r *TUFRepository) Refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx, _, err := r.loadCatalog(ctx, true)
	if err == nil {
		r.index = idx
	}
	return err
}
func (r *TUFRepository) List(ctx context.Context) ([]CatalogEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.index != nil {
		return flattenCatalog(r.index), nil
	}
	idx, _, err := r.loadCatalog(ctx, false)
	if err != nil {
		return nil, err
	}
	r.index = idx
	return flattenCatalog(idx), nil
}
func (r *TUFRepository) Resolve(ctx context.Context, id, version string) (CatalogEntry, error) {
	entries, err := r.List(ctx)
	if err != nil {
		return CatalogEntry{}, err
	}
	for _, e := range entries {
		if e.ID == id && (version == "" || e.Version == version) {
			return e, nil
		}
	}
	return CatalogEntry{}, ErrPluginNotFound
}
func (r *TUFRepository) Fetch(ctx context.Context, entry CatalogEntry, destination string) error {
	if entry.Access == CatalogAccessLicensed {
		return ErrLicenseRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	up, _, err := r.newUpdater(ctx, true)
	if err != nil {
		return err
	}
	if err = up.Refresh(); err != nil {
		return fmt.Errorf("trusted plugin metadata unavailable: %w", err)
	}
	info, err := up.GetTargetInfo(entry.Package)
	if err != nil {
		return err
	}
	if err = verifyCatalogTarget(entry, info); err != nil {
		return err
	}
	return r.downloadAtomic(ctx, up, info, destination, 0o640)
}

// FetchResolved downloads a service-authorized URL while keeping the trusted
// TUF target length and hashes authoritative.
func (r *TUFRepository) FetchResolved(ctx context.Context, entry CatalogEntry, resolved DownloadResolveResponse, destination string) error {
	if resolved.Target != entry.Package || resolved.SHA256 != entry.PackageSHA256 || resolved.Length != entry.Size {
		return ErrPluginIDMismatch
	}
	if resolved.ExpiresAt.IsZero() || !resolved.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("authorized plugin download URL is expired")
	}
	u, err := url.Parse(resolved.DownloadURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(r.cfg.AllowHTTP && u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
		return errors.New("invalid authorized plugin download URL")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	up, _, err := r.newUpdater(ctx, true)
	if err != nil {
		return err
	}
	if err = up.Refresh(); err != nil {
		return fmt.Errorf("trusted plugin metadata unavailable: %w", err)
	}
	info, err := up.GetTargetInfo(entry.Package)
	if err != nil {
		return err
	}
	if err = verifyCatalogTarget(entry, info); err != nil {
		return err
	}
	if _, err := os.Stat(destination); err == nil {
		return os.ErrExist
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".plugin-*")
	if err != nil {
		return err
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolved.DownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := r.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("authorized download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, info.Length+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > info.Length {
		return errors.New("authorized plugin download exceeds trusted length")
	}
	if err = info.VerifyLengthHashes(data); err != nil {
		return err
	}
	if err = os.WriteFile(path, data, 0o640); err != nil {
		return err
	}
	return os.Rename(path, destination)
}

func (r *TUFRepository) loadCatalog(ctx context.Context, online bool) (*CatalogIndex, bool, error) {
	up, recorder, err := r.newUpdater(ctx, !online)
	if err != nil {
		return nil, false, err
	}
	usingCache := !online
	refreshErr := up.Refresh()
	if refreshErr != nil && online {
		up, recorder, err = r.newUpdater(ctx, true)
		if err == nil {
			err = up.Refresh()
		}
		if err != nil {
			r.status.LastRefreshError = refreshErr.Error()
			return nil, false, fmt.Errorf("official repository refresh failed and trusted cache is unavailable: %w", errors.Join(refreshErr, err))
		}
		usingCache = true
	} else if refreshErr != nil {
		return nil, false, refreshErr
	}
	if !usingCache {
		if err = r.persistRecordedRoots(recorder.roots); err != nil {
			return nil, false, err
		}
	}
	info, err := up.GetTargetInfo(CatalogTargetPath)
	if err != nil {
		return nil, false, fmt.Errorf("catalog target missing: %w", err)
	}
	cachePath := filepath.Join(r.cfg.CacheDir, "targets", "catalog-v1-index.json")
	var data []byte
	if usingCache {
		data, err = os.ReadFile(cachePath)
		if err == nil {
			err = info.VerifyLengthHashes(data)
		}
	} else {
		data, err = r.downloadBytesAtomic(ctx, up, info, cachePath)
	}
	if err != nil {
		return nil, false, fmt.Errorf("load trusted catalog target: %w", err)
	}
	idx, err := parseCatalogIndex(data)
	if err != nil {
		return nil, false, err
	}
	r.status.UsingCache = usingCache
	r.status.CatalogVersion = idx.Version
	if online && !usingCache {
		r.status.LastRefreshAt = time.Now().UTC()
		r.status.LastRefreshError = ""
	} else if online && refreshErr != nil {
		r.status.LastRefreshError = refreshErr.Error()
	}
	return idx, usingCache, nil
}

type recordingFetcher struct {
	ctx    context.Context
	client *http.Client
	roots  map[int64][]byte
}

func (f *recordingFetcher) DownloadFile(raw string, max int64, _ time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: resp.StatusCode, URL: raw}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: "repository response exceeds trusted length limit"}
	}
	var v int64
	if _, err = fmt.Sscanf(filepath.Base(req.URL.Path), "%d.root.json", &v); err == nil && v > 0 {
		f.roots[v] = append([]byte(nil), data...)
	}
	return data, nil
}

var _ fetcher.Fetcher = (*recordingFetcher)(nil)

func (r *TUFRepository) newUpdater(ctx context.Context, offline bool) (*updater.Updater, *recordingFetcher, error) {
	root, err := r.latestTrustedRoot()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := tufconfig.New(r.cfg.MetadataURL, root)
	if err != nil {
		return nil, nil, err
	}
	rec := &recordingFetcher{ctx: ctx, client: r.cfg.HTTPClient, roots: map[int64][]byte{}}
	cfg.Fetcher = rec
	cfg.LocalMetadataDir = filepath.Join(r.cfg.CacheDir, "metadata")
	cfg.LocalTargetsDir = filepath.Join(r.cfg.CacheDir, "targets")
	cfg.RemoteTargetsURL = r.cfg.TargetsURL
	cfg.PrefixTargetsWithHash = true
	cfg.UnsafeLocalMode = offline
	up, err := updater.New(cfg)
	return up, rec, err
}
func (r *TUFRepository) latestTrustedRoot() ([]byte, error) {
	trusted, err := trustedmetadata.New(r.cfg.BootstrapRoot)
	if err != nil {
		return nil, err
	}
	latest := append([]byte(nil), r.cfg.BootstrapRoot...)
	for v := trusted.Root.Signed.Version + 1; ; v++ {
		data, err := os.ReadFile(filepath.Join(r.cfg.CacheDir, "roots", fmt.Sprintf("%d.root.json", v)))
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		if _, err = trusted.UpdateRoot(data); err != nil {
			return nil, fmt.Errorf("cached root %d failed rotation verification: %w", v, err)
		}
		latest = data
	}
	return latest, nil
}
func (r *TUFRepository) persistRecordedRoots(roots map[int64][]byte) error {
	for v, data := range roots {
		if err := writeAtomic(filepath.Join(r.cfg.CacheDir, "roots", fmt.Sprintf("%d.root.json", v)), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (r *TUFRepository) downloadBytesAtomic(ctx context.Context, up *updater.Updater, info *metadata.TargetFiles, dst string) ([]byte, error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".target-*")
	if err != nil {
		return nil, err
	}
	p := tmp.Name()
	if err = tmp.Close(); err != nil {
		return nil, err
	}
	defer os.Remove(p)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	_, data, err := up.DownloadTarget(info, p, "")
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(p, 0o600); err != nil {
		return nil, err
	}
	if err = os.Rename(p, dst); err != nil {
		return nil, err
	}
	return data, nil
}
func (r *TUFRepository) downloadAtomic(ctx context.Context, up *updater.Updater, info *metadata.TargetFiles, dst string, mode os.FileMode) error {
	if _, err := os.Stat(dst); err == nil {
		return os.ErrExist
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".plugin-*")
	if err != nil {
		return err
	}
	p := tmp.Name()
	if err = tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(p)
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, _, err = up.DownloadTarget(info, p, ""); err != nil {
		return err
	}
	if err = os.Chmod(p, mode); err != nil {
		return err
	}
	return os.Rename(p, dst)
}
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".atomic-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func parseCatalogIndex(data []byte) (*CatalogIndex, error) {
	var idx CatalogIndex
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("decode catalog index: %w", err)
	}
	if idx.SchemaVersion != CatalogSchemaVersion || idx.Version < 1 || idx.GeneratedAt.IsZero() {
		return nil, errors.New("catalog index header is invalid")
	}
	ids := map[string]bool{}
	targets := map[string]bool{}
	for _, p := range idx.Plugins {
		if !pluginIDPattern.MatchString(p.ID) || ids[p.ID] || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Publisher) == "" {
			return nil, errors.New("catalog contains an invalid or duplicate plugin")
		}
		ids[p.ID] = true
		if p.Access != CatalogAccessFree && p.Access != CatalogAccessLicensed {
			return nil, fmt.Errorf("plugin %s has invalid access mode", p.ID)
		}
		versions := map[string]bool{}
		for _, v := range p.Versions {
			if v.Version == "" || v.Length < 0 || v.Length > DefaultPackageLimits.CompressedBytes || !isTargetPath(v.Target) || versions[v.Version] || targets[v.Target] {
				return nil, fmt.Errorf("plugin %s has invalid catalog version", p.ID)
			}
			if _, err := hex.DecodeString(v.SHA256); err != nil || len(v.SHA256) != sha256.Size*2 {
				return nil, fmt.Errorf("plugin %s has invalid package digest", p.ID)
			}
			versions[v.Version] = true
			targets[v.Target] = true
		}
	}
	return &idx, nil
}
func flattenCatalog(idx *CatalogIndex) []CatalogEntry {
	out := []CatalogEntry{}
	for _, p := range idx.Plugins {
		for _, v := range p.Versions {
			out = append(out, CatalogEntry{ID: p.ID, Version: v.Version, Package: v.Target, PackageSHA256: v.SHA256, Size: v.Length, Name: p.Name, Summary: p.Summary, Description: p.Description, Publisher: p.Publisher, Icon: p.Icon, Permissions: p.Permissions, Providers: p.Providers, Access: p.Access})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Version > out[j].Version
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func verifyCatalogTarget(e CatalogEntry, info *metadata.TargetFiles) error {
	if info == nil || info.Length != e.Size {
		return ErrPluginIDMismatch
	}
	want, err := hex.DecodeString(e.PackageSHA256)
	if err != nil || !equalBytes(info.Hashes["sha256"], want) {
		return ErrPluginIDMismatch
	}
	return nil
}
func isTargetPath(v string) bool {
	return v != "" && !strings.HasPrefix(v, "/") && !strings.Contains(v, "\\") && filepath.ToSlash(filepath.Clean(v)) == v && !strings.HasPrefix(v, "../")
}
func equalBytes(a, b []byte) bool {
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
