package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	panelnginx "github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/waf"
)

const (
	wafModuleName        = "ngx_http_modsecurity_module.so"
	wafProviderMetaName  = "provider.json"
	wafMaxModuleBytes    = int64(128 * 1024 * 1024)
	wafMaxMainConfigSize = int64(16 * 1024 * 1024)
)

type wafRuntimeResponse struct {
	Fingerprint       waf.RuntimeFingerprint `json:"fingerprint"`
	BuiltinCandidates []wafModuleCandidate   `json:"builtin_candidates"`
}

type wafModuleCandidate struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type wafInstallRequest struct {
	ProviderID         string                 `json:"provider_id"`
	ABIVersion         int                    `json:"abi_version"`
	Version            string                 `json:"version"`
	OperationID        string                 `json:"operation_id"`
	ArtifactPath       string                 `json:"artifact_path"`
	ArtifactSHA256     string                 `json:"artifact_sha256"`
	RuntimeFingerprint waf.RuntimeFingerprint `json:"runtime_fingerprint"`
}

type wafProviderMetadata struct {
	ProviderID         string                 `json:"provider_id"`
	ABIVersion         int                    `json:"abi_version"`
	Version            string                 `json:"version"`
	ModuleSHA256       string                 `json:"module_sha256"`
	RuntimeFingerprint waf.RuntimeFingerprint `json:"runtime_fingerprint"`
}

type wafActivateRequest struct {
	ProviderID         string                 `json:"provider_id"`
	ABIVersion         int                    `json:"abi_version"`
	Version            string                 `json:"version"`
	OperationID        string                 `json:"operation_id"`
	RuntimeFingerprint waf.RuntimeFingerprint `json:"runtime_fingerprint"`
}

type wafSiteApplyRequest struct {
	ProviderID     string         `json:"provider_id"`
	ABIVersion     int            `json:"abi_version"`
	OperationID    string         `json:"operation_id"`
	SiteConfigPath string         `json:"site_config_path"`
	RulesVersion   string         `json:"rules_version"`
	Policy         waf.SitePolicy `json:"policy"`
}

func (s *Server) handleWAFRuntimeDetect(w http.ResponseWriter, r *http.Request) {
	fingerprint, err := s.executor.WAFFingerprint(r.Context())
	if err != nil {
		writeAgentError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeAgentOK(w, wafRuntimeResponse{Fingerprint: fingerprint, BuiltinCandidates: detectBuiltinWAFModules()})
}

func detectBuiltinWAFModules() []wafModuleCandidate {
	paths := []string{
		"/usr/lib/nginx/modules/" + wafModuleName,
		"/usr/lib64/nginx/modules/" + wafModuleName,
		"/usr/local/openresty/nginx/modules/" + wafModuleName,
	}
	result := make([]wafModuleCandidate, 0, len(paths))
	for _, path := range paths {
		sha, err := hashRegularFile(path, wafMaxModuleBytes)
		if err == nil {
			result = append(result, wafModuleCandidate{Path: path, SHA256: sha})
		}
	}
	return result
}

func (s *Server) handleWAFProviderInstall(w http.ResponseWriter, r *http.Request) {
	var req wafInstallRequest
	if err := decodeWAFJSON(r, &req); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := waf.ValidateProvider(req.ProviderID, req.ABIVersion); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := waf.ValidateVersion(req.Version); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateOperationID(req.OperationID); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.executor.WAFFingerprint(r.Context())
	if err != nil {
		writeAgentError(w, http.StatusUnprocessableEntity, "runtime fingerprint failed: "+err.Error())
		return
	}
	if err := requireExactFingerprint(req.RuntimeFingerprint, current); err != nil {
		writeAgentError(w, http.StatusConflict, err.Error())
		return
	}
	source, err := s.resolveWAFArtifact(req.ArtifactPath)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, err.Error())
		return
	}
	module, err := readBoundedRegularFile(source, wafMaxModuleBytes)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "invalid provider artifact: "+err.Error())
		return
	}
	if err := validateWAFModuleELF(module, runtime.GOARCH); err != nil {
		writeAgentError(w, http.StatusBadRequest, "invalid provider artifact: "+err.Error())
		return
	}
	sum := sha256.Sum256(module)
	actualSHA := hex.EncodeToString(sum[:])
	if !validSHA256(req.ArtifactSHA256) || !strings.EqualFold(actualSHA, req.ArtifactSHA256) {
		writeAgentError(w, http.StatusBadRequest, "provider artifact digest mismatch")
		return
	}
	versionDir, _ := waf.ProviderVersionDir(s.cfg.Nginx.PanelDir, req.Version)
	metadata := wafProviderMetadata{
		ProviderID: req.ProviderID, ABIVersion: req.ABIVersion, Version: req.Version,
		ModuleSHA256: actualSHA, RuntimeFingerprint: current,
	}
	metadataBytes, _ := json.MarshalIndent(metadata, "", "  ")
	changes := []FileChange{
		{Type: "write", Path: filepath.Join(versionDir, wafModuleName), Content: module, Perm: 0755},
		{Type: "write", Path: filepath.Join(versionDir, wafProviderMetaName), Content: append(metadataBytes, '\n'), Perm: 0644},
	}
	if err := s.applyWAFChanges(r.Context(), req.OperationID, changes, false); err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"installed": true, "version": req.Version, "module_sha256": actualSHA})
}

func (s *Server) resolveWAFArtifact(requested string) (string, error) {
	resolved, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("resolve provider artifact: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	staging := filepath.Join(s.cfg.Nginx.PanelDir, "plugins", ".staging")
	if isPathWithin(staging, resolved) {
		return resolved, nil
	}
	for _, candidate := range detectBuiltinWAFModules() {
		if resolved == candidate.Path {
			return resolved, nil
		}
	}
	return "", errors.New("provider artifact must be in the plugin staging directory or a fixed built-in module path")
}

func (s *Server) handleWAFProviderActivate(w http.ResponseWriter, r *http.Request) {
	var req wafActivateRequest
	if err := decodeWAFJSON(r, &req); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := waf.ValidateProvider(req.ProviderID, req.ABIVersion); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateOperationID(req.OperationID); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	versionDir, err := waf.ProviderVersionDir(s.cfg.Nginx.PanelDir, req.Version)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.executor.WAFFingerprint(r.Context())
	if err != nil {
		writeAgentError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := requireExactFingerprint(req.RuntimeFingerprint, current); err != nil {
		writeAgentError(w, http.StatusConflict, err.Error())
		return
	}
	metadata, err := readWAFMetadata(filepath.Join(versionDir, wafProviderMetaName))
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "provider is not installed: "+err.Error())
		return
	}
	if metadata.ProviderID != req.ProviderID || metadata.ABIVersion != req.ABIVersion || metadata.Version != req.Version {
		writeAgentError(w, http.StatusConflict, "installed provider metadata mismatch")
		return
	}
	if err := requireExactFingerprint(metadata.RuntimeFingerprint, current); err != nil {
		writeAgentError(w, http.StatusConflict, "installed artifact is incompatible: "+err.Error())
		return
	}
	modulePath := filepath.Join(versionDir, wafModuleName)
	moduleSHA, err := hashRegularFile(modulePath, wafMaxModuleBytes)
	if err != nil || !strings.EqualFold(moduleSHA, metadata.ModuleSHA256) {
		writeAgentError(w, http.StatusConflict, "installed provider module digest mismatch")
		return
	}
	confPath := s.executor.GetConfPath()
	conf, err := readBoundedRegularFile(confPath, wafMaxMainConfigSize)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "read nginx config: "+err.Error())
		return
	}
	updated, err := replaceWAFLoadModuleBlock(conf, modulePath)
	if err != nil {
		writeAgentError(w, http.StatusConflict, err.Error())
		return
	}
	activeBytes, _ := json.MarshalIndent(metadata, "", "  ")
	changes := []FileChange{
		{Type: "write", Path: confPath, Content: updated, Perm: 0644},
		{Type: "write", Path: filepath.Join(s.cfg.Nginx.PanelDir, "waf", "active-provider.json"), Content: append(activeBytes, '\n'), Perm: 0644},
	}
	if err := s.applyWAFChanges(r.Context(), req.OperationID, changes, true); err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"activated": true, "version": req.Version})
}

func (s *Server) handleWAFSiteApply(w http.ResponseWriter, r *http.Request) {
	var req wafSiteApplyRequest
	if err := decodeWAFJSON(r, &req); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := waf.ValidateProvider(req.ProviderID, req.ABIVersion); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateOperationID(req.OperationID); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := waf.ValidateVersion(req.RulesVersion); err != nil {
		writeAgentError(w, http.StatusBadRequest, "invalid rules_version: "+err.Error())
		return
	}
	providerRoot := filepath.Join(s.cfg.Nginx.PanelDir, "waf")
	active, err := readWAFMetadata(filepath.Join(providerRoot, "active-provider.json"))
	if err != nil || active.ProviderID != req.ProviderID || active.ABIVersion != req.ABIVersion {
		writeAgentError(w, http.StatusConflict, "WAF provider is not active")
		return
	}
	current, err := s.executor.WAFFingerprint(r.Context())
	if err != nil || requireExactFingerprint(active.RuntimeFingerprint, current) != nil {
		writeAgentError(w, http.StatusConflict, "active WAF provider is incompatible with the current runtime")
		return
	}
	rulesRoot := filepath.Join(providerRoot, "rules", "versions", req.RulesVersion)
	req.Policy.ModSecurityConf = filepath.Join(providerRoot, "modsecurity.conf")
	req.Policy.CRSSetupPath = filepath.Join(rulesRoot, "crs-setup.conf")
	req.Policy.CRSRulesGlob = filepath.Join(rulesRoot, "rules", "*.conf")
	req.Policy.Audit.StorageDir = filepath.Join(providerRoot, "audit", req.Policy.SiteID)
	content, err := waf.RenderSiteConfig(req.Policy, []string{providerRoot})
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	path, _ := waf.SiteConfigPath(s.cfg.Nginx.PanelDir, req.Policy.SiteID)
	if _, err := s.policy.Validate(req.SiteConfigPath); err != nil {
		writeAgentError(w, http.StatusForbidden, "site config path is outside the agent policy")
		return
	}
	siteConfig, err := readBoundedRegularFile(req.SiteConfigPath, wafMaxMainConfigSize)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "read site config: "+err.Error())
		return
	}
	includeBody := []byte("    include '" + path + "';\n")
	updatedSiteConfig, err := panelnginx.EnsureMarkerBlock(siteConfig, panelnginx.MarkerNameWAF, includeBody)
	if err != nil {
		writeAgentError(w, http.StatusConflict, "patch site WAF marker: "+err.Error())
		return
	}
	if err := s.prepareWAFAuditDirectory(req.Policy.Audit.StorageDir); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "prepare WAF audit directory: "+err.Error())
		return
	}
	changes := []FileChange{
		{Type: "write", Path: path, Content: content, Perm: 0640},
		{Type: "write", Path: req.SiteConfigPath, Content: updatedSiteConfig, Perm: 0644},
	}
	if err := s.applyWAFChanges(r.Context(), req.OperationID, changes, true); err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"applied": true, "include_path": path})
}

// Only the site directory is worker-owned. Its parent remains agent-owned,
// allowing traversal by the worker group without granting rename/delete access.
func (s *Server) prepareWAFAuditDirectory(dir string) error {
	if s.cfg.Nginx.WebUser == "" || s.cfg.Nginx.WebGroup == "" {
		return errors.New("nginx worker user and group must be configured")
	}
	uid, err := resolveUser(s.cfg.Nginx.WebUser)
	if err != nil {
		return err
	}
	gid, err := resolveGroup(s.cfg.Nginx.WebGroup)
	if err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		_, err := s.policy.Validate(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0750); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("WAF audit directory cannot use symlinks")
		}
		owner := os.Geteuid()
		if path == dir {
			owner = uid
		}
		if err := os.Chown(path, owner, gid); err != nil {
			return err
		}
		if err := os.Chmod(path, 0750); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) applyWAFChanges(ctx context.Context, operationID string, changes []FileChange, validateNginx bool) error {
	tx, err := NewTransaction(operationID, filepath.Join(s.cfg.Nginx.PanelDir, "backups"), s.policy, s.cfg.Nginx.WebUser, s.cfg.Nginx.WebGroup)
	if err != nil {
		return fmt.Errorf("create WAF transaction: %w", err)
	}
	if err := tx.Apply(ctx, changes); err != nil {
		return fmt.Errorf("apply WAF transaction: %w", err)
	}
	if !validateNginx {
		return nil
	}
	if result, err := s.executor.Test(ctx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return fmt.Errorf("nginx -t failed (rolled back): %s", commandError(result, err))
	}
	if result, err := s.executor.Reload(ctx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		rollbackCtx, cancel := context.WithTimeout(context.Background(), s.timeouts.Reload)
		_, _ = s.executor.Reload(rollbackCtx)
		cancel()
		return fmt.Errorf("nginx reload failed (rolled back): %s", commandError(result, err))
	}
	return nil
}

func commandError(result CmdResult, err error) string {
	if strings.TrimSpace(result.Stderr) != "" {
		return strings.TrimSpace(result.Stderr)
	}
	return err.Error()
}

func requireExactFingerprint(got, current waf.RuntimeFingerprint) error {
	if err := got.Validate(); err != nil {
		return fmt.Errorf("invalid runtime fingerprint: %w", err)
	}
	if !strings.EqualFold(got.Digest, current.Digest) {
		return fmt.Errorf("runtime fingerprint mismatch: artifact=%s runtime=%s", got.Digest, current.Digest)
	}
	return nil
}

func readWAFMetadata(path string) (wafProviderMetadata, error) {
	b, err := readBoundedRegularFile(path, 64*1024)
	if err != nil {
		return wafProviderMetadata{}, err
	}
	var out wafProviderMetadata
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return wafProviderMetadata{}, err
	}
	return out, nil
}

func readBoundedRegularFile(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > max {
		return nil, errors.New("file is not a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("file exceeds size limit")
	}
	return data, nil
}

func validateWAFModuleELF(module []byte, goarch string) error {
	f, err := elf.NewFile(bytes.NewReader(module))
	if err != nil {
		return fmt.Errorf("module is not ELF: %w", err)
	}
	defer f.Close()
	if f.Type != elf.ET_DYN {
		return errors.New("module is not an ELF shared object")
	}
	want := map[string]elf.Machine{
		"amd64": elf.EM_X86_64,
		"arm64": elf.EM_AARCH64,
		"386":   elf.EM_386,
		"arm":   elf.EM_ARM,
	}[goarch]
	if want == elf.EM_NONE || f.Machine != want {
		return fmt.Errorf("module architecture %s does not match runtime %s", f.Machine, goarch)
	}
	return nil
}

func replaceWAFLoadModuleBlock(conf []byte, modulePath string) ([]byte, error) {
	if strings.ContainsAny(modulePath, "\r\n'\\") {
		return nil, errors.New("unsafe module path")
	}
	const begin = "# nxpanel-waf-provider begin"
	const end = "# nxpanel-waf-provider end"
	text := string(conf)
	beginIndex, endIndex := strings.Index(text, begin), strings.Index(text, end)
	if (beginIndex >= 0) != (endIndex >= 0) || (beginIndex >= 0 && endIndex < beginIndex) {
		return nil, errors.New("malformed nxPanel WAF provider marker")
	}
	block := begin + "\nload_module '" + modulePath + "';\n" + end + "\n"
	if beginIndex < 0 {
		return []byte(block + text), nil
	}
	endIndex += len(end)
	if endIndex < len(text) && text[endIndex] == '\r' {
		endIndex++
	}
	if endIndex < len(text) && text[endIndex] == '\n' {
		endIndex++
	}
	return []byte(text[:beginIndex] + block + text[endIndex:]), nil
}

func decodeWAFJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isPathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
