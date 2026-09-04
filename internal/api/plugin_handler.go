package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/plugin"
)

type PluginNativeAdapter interface {
	BeforeEnable(*http.Request, *plugin.Installation) error
	BeforeDisable(*http.Request, *plugin.Installation) error
	Invoke(http.ResponseWriter, *http.Request, *plugin.Installation, pluginRPCRequest) (bool, any, error)
}

type PluginHandler struct {
	service       *plugin.Service
	nativeAdapter PluginNativeAdapter
}

func NewPluginHandler(service *plugin.Service) *PluginHandler {
	return &PluginHandler{service: service}
}

func RegisterPluginRoutes(r chi.Router, h *PluginHandler) {
	r.Get("/plugins/catalog", h.catalog)
	r.Post("/plugins/catalog/refresh", h.refresh)
	r.Get("/plugins/repository/status", h.repositoryStatus)
	r.Get("/plugins/developer-mode", h.developerMode)
	r.Put("/plugins/developer-mode", h.setDeveloperMode)
	r.Post("/plugins/developer/packages/inspect", h.inspectDeveloperPackage)
	r.Post("/plugins/developer/packages/install", h.installDeveloperPackage)
	r.Get("/plugins/authorizations", h.authorizations)
	r.Post("/plugins/authorizations/device", h.startDeviceAuthorization)
	r.Post("/plugins/authorizations/device/{attempt_id}/poll", h.pollDeviceAuthorization)
	r.Delete("/plugins/authorizations/{authorization_id}", h.revokeAuthorization)
	r.Get("/plugins", h.list)
	r.Get("/plugins/contributions", h.contributions)
	r.Get("/plugins/{plugin_id}", h.get)
	r.Post("/plugins/{plugin_id}/install", h.install)
	r.Post("/plugins/{plugin_id}/update", h.install)
	r.Put("/plugins/{plugin_id}/authorization", h.bindAuthorization)
	r.Post("/plugins/{plugin_id}/enable", h.enable)
	r.Post("/plugins/{plugin_id}/disable", h.disable)
	r.Post("/plugins/{plugin_id}/rpc", h.rpc)
	r.Delete("/plugins/{plugin_id}", h.remove)
	r.Get("/plugins/{plugin_id}/ui/*", h.uiDocument)
	r.Get("/plugins/{plugin_id}/assets/*", h.uiAsset)
}

func (h *PluginHandler) repositoryStatus(w http.ResponseWriter, r *http.Request) {
	WriteOK(w, r, h.service.RepositoryStatus())
}

type pluginCatalogDTO struct {
	plugin.CatalogEntry
	InstalledVersion string `json:"installed_version,omitempty"`
	State            string `json:"state,omitempty"`
	Health           string `json:"health,omitempty"`
	Compatible       bool   `json:"compatible"`
	UpdateAvailable  bool   `json:"update_available,omitempty"`
}

type pluginInstallationDTO struct {
	ID                    string                        `json:"id"`
	Name                  string                        `json:"name"`
	Summary               string                        `json:"summary"`
	Publisher             string                        `json:"publisher"`
	Version               string                        `json:"version"`
	Enabled               bool                          `json:"enabled"`
	State                 string                        `json:"state"`
	Health                string                        `json:"health"`
	Compatible            bool                          `json:"compatible"`
	Permissions           []plugin.PermissionDescriptor `json:"permissions"`
	Approved              []string                      `json:"approved_permissions"`
	Providers             []string                      `json:"required_providers,omitempty"`
	InstalledAt           any                           `json:"installed_at,omitempty"`
	UpdatedAt             any                           `json:"updated_at,omitempty"`
	LastError             string                        `json:"last_error,omitempty"`
	Source                string                        `json:"source"`
	VerificationStatus    string                        `json:"verification_status"`
	SourceConflict        bool                          `json:"source_conflict,omitempty"`
	DeveloperModeRequired bool                          `json:"developer_mode_required,omitempty"`
}

func (h *PluginHandler) catalog(w http.ResponseWriter, r *http.Request) {
	entries, err := h.service.Catalog(r.Context())
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	installed, _ := h.service.List(r.Context())
	byID := make(map[string]plugin.Installation)
	for _, item := range installed {
		byID[item.ID] = item
	}
	out := make([]pluginCatalogDTO, 0, len(entries))
	for _, entry := range entries {
		d := pluginCatalogDTO{CatalogEntry: entry, Compatible: true}
		if item, ok := byID[entry.ID]; ok {
			d.InstalledVersion = item.ActiveVersion
			d.Health = item.HealthStatus
			if item.Enabled {
				d.State = "enabled"
			} else {
				d.State = "disabled"
			}
			d.UpdateAvailable = item.ActiveVersion != entry.Version
		}
		out = append(out, d)
	}
	WriteOK(w, r, map[string]any{"items": out})
}
func (h *PluginHandler) refresh(w http.ResponseWriter, r *http.Request) {
	err := h.service.RefreshCatalog(r.Context())
	h.respond(w, r, map[string]bool{"refreshed": err == nil}, err)
}
func (h *PluginHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context())
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	out := make([]pluginInstallationDTO, 0, len(items))
	for i := range items {
		out = append(out, installationDTO(items[i]))
	}
	WriteOK(w, r, map[string]any{"items": out})
}
func (h *PluginHandler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Get(r.Context(), chi.URLParam(r, "plugin_id"))
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	h.respond(w, r, installationDTO(*item), nil)
}
func installationDTO(item plugin.Installation) pluginInstallationDTO {
	m := item.Manifest
	d := pluginInstallationDTO{ID: item.ID, Version: item.ActiveVersion, Enabled: item.Enabled, State: "disabled", Health: item.HealthStatus, Compatible: true, Approved: item.Permissions, InstalledAt: item.InstalledAt, UpdatedAt: item.UpdatedAt, LastError: item.LastError, Source: item.Source, VerificationStatus: item.VerificationStatus, SourceConflict: item.SourceConflict, DeveloperModeRequired: item.Source == "developer" && !item.Enabled}
	if item.Enabled {
		d.State = "enabled"
	}
	if m != nil {
		d.Name = m.Name
		d.Summary = m.Name
		d.Publisher = m.Publisher
		d.Providers = m.Providers
		for _, p := range m.Permissions {
			d.Permissions = append(d.Permissions, plugin.PermissionDescriptor{Name: p})
		}
	}
	return d
}

func (h *PluginHandler) developerMode(w http.ResponseWriter, r *http.Request) {
	enabled, err := h.service.DeveloperMode(r.Context())
	h.respond(w, r, map[string]bool{"enabled": enabled}, err)
}

func (h *PluginHandler) setDeveloperMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled         bool `json:"enabled"`
		AcknowledgeRisk bool `json:"acknowledge_risk"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	err := h.service.SetDeveloperMode(r.Context(), req.Enabled, req.AcknowledgeRisk)
	h.respond(w, r, map[string]bool{"enabled": req.Enabled}, err)
}

func (h *PluginHandler) inspectDeveloperPackage(w http.ResponseWriter, r *http.Request) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		h.respond(w, r, nil, errors.New("invalid multipart developer package upload"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, plugin.DefaultPackageLimits.CompressedBytes+64*1024)
	mr := multipart.NewReader(r.Body, params["boundary"])
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		if part != nil {
			_ = part.Close()
		}
		h.respond(w, r, nil, errors.New("exactly one file part is required"))
		return
	}
	filename := filepath.Base(part.FileName())
	inspection, inspectErr := h.service.InspectDeveloperPackage(r.Context(), filename, part)
	_ = part.Close()
	if inspectErr == nil {
		if next, nextErr := mr.NextPart(); !errors.Is(nextErr, io.EOF) {
			if next != nil {
				_ = next.Close()
			}
			inspectErr = errors.New("exactly one file part is required")
			if inspection != nil {
				h.service.DiscardDeveloperPackage(inspection.UploadToken)
			}
		}
	}
	h.respond(w, r, inspection, inspectErr)
}

func (h *PluginHandler) installDeveloperPackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UploadToken         string   `json:"upload_token"`
		ApprovedPermissions []string `json:"approved_permissions"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.UploadToken) == "" {
		h.respond(w, r, nil, errors.New("invalid upload token"))
		return
	}
	item, err := h.service.InstallDeveloperPackage(r.Context(), req.UploadToken, req.ApprovedPermissions)
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	WriteCreated(w, r, installationDTO(*item))
}

func (h *PluginHandler) authorizations(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Authorizations(r.Context())
	h.respond(w, r, map[string]any{"items": items}, err)
}
func (h *PluginHandler) startDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PluginID string `json:"plugin_id"`
		Version  string `json:"version"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	attempt, err := h.service.StartDeviceAuthorization(r.Context(), req.PluginID, req.Version)
	h.respond(w, r, attempt, err)
}
func (h *PluginHandler) pollDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.PollDeviceAuthorization(r.Context(), chi.URLParam(r, "attempt_id"))
	h.respond(w, r, result, err)
}
func (h *PluginHandler) bindAuthorization(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AuthorizationID string `json:"authorization_id"`
		Version         string `json:"version,omitempty"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	err := h.service.BindAuthorization(r.Context(), chi.URLParam(r, "plugin_id"), req.Version, req.AuthorizationID)
	h.respond(w, r, map[string]bool{"bound": err == nil}, err)
}
func (h *PluginHandler) revokeAuthorization(w http.ResponseWriter, r *http.Request) {
	err := h.service.RevokeAuthorization(r.Context(), chi.URLParam(r, "authorization_id"))
	h.respond(w, r, map[string]bool{"revoked": err == nil}, err)
}
func (h *PluginHandler) install(w http.ResponseWriter, r *http.Request) {
	var req plugin.InstallRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.ID = chi.URLParam(r, "plugin_id")
	item, err := h.service.Install(r.Context(), req)
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	h.respond(w, r, map[string]any{"operation_id": middleware.GetRequestID(r.Context()), "status": "success", "plugin": installationDTO(*item)}, nil)
}
func (h *PluginHandler) enable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "plugin_id")
	item, err := h.service.Get(r.Context(), id)
	if err == nil && h.nativeAdapter != nil {
		err = h.nativeAdapter.BeforeEnable(r, item)
	}
	if err == nil {
		_, err = h.service.Enable(r.Context(), id)
	}
	h.operation(w, r, err)
}
func (h *PluginHandler) disable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "plugin_id")
	item, err := h.service.Get(r.Context(), id)
	if err == nil && h.nativeAdapter != nil {
		err = h.nativeAdapter.BeforeDisable(r, item)
	}
	if err == nil {
		_, err = h.service.Disable(r.Context(), id)
	}
	h.operation(w, r, err)
}
func (h *PluginHandler) remove(w http.ResponseWriter, r *http.Request) {
	err := h.service.Delete(r.Context(), chi.URLParam(r, "plugin_id"))
	h.operation(w, r, err)
}
func (h *PluginHandler) operation(w http.ResponseWriter, r *http.Request, err error) {
	h.respond(w, r, map[string]any{"operation_id": middleware.GetRequestID(r.Context()), "status": "success"}, err)
}

func (h *PluginHandler) contributions(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Contributions(r.Context())
	h.respond(w, r, map[string]any{"items": items}, err)
}

type pluginRPCRequest struct {
	Method  string                     `json:"method"`
	Payload json.RawMessage            `json:"payload,omitempty"`
	Context map[string]json.RawMessage `json:"context,omitempty"`
}

func (h *PluginHandler) rpc(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "plugin_id")
	item, err := h.service.Get(r.Context(), id)
	if err != nil || !item.Enabled {
		if err == nil {
			err = errors.New("plugin is disabled")
		}
		h.respond(w, r, nil, err)
		return
	}
	var req pluginRPCRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if item.Manifest == nil || !declaresRPCMethod(item.Manifest, req.Method) {
		h.respond(w, r, nil, errors.New("plugin RPC method is not available"))
		return
	}
	if h.nativeAdapter != nil {
		handled, data, nativeErr := h.nativeAdapter.Invoke(w, r, item, req)
		if handled {
			h.respond(w, r, data, nativeErr)
			return
		}
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	data, invokeErr := h.service.Invoke(r.Context(), id, req.Method, payload)
	if invokeErr != nil {
		h.respond(w, r, nil, invokeErr)
		return
	}
	if !json.Valid(data) {
		h.respond(w, r, nil, errors.New("plugin returned invalid JSON"))
		return
	}
	WriteOK(w, r, json.RawMessage(data))
}

func declaresRPCMethod(manifest *plugin.Manifest, method string) bool {
	if method == "" || len(method) > 256 {
		return false
	}
	for _, contribution := range manifest.Contributions {
		if contains(contribution.RPCMethods, method) {
			return true
		}
	}
	return false
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

var pluginFrame = template.Must(template.New("frame").Parse(`<!doctype html><html><head><meta charset="utf-8">{{if .Style}}<link rel="stylesheet" href="{{.Style}}">{{end}}</head><body><div id="nxpanel-plugin-root"></div><script src="{{.Script}}"></script></body></html>`))

func (h *PluginHandler) uiDocument(w http.ResponseWriter, r *http.Request) {
	id, entry := chi.URLParam(r, "plugin_id"), chi.URLParam(r, "*")
	item, err := h.service.Get(r.Context(), id)
	if err != nil || !item.Enabled || item.Manifest == nil || item.Manifest.UI == nil || entry != item.Manifest.UI.Script {
		http.NotFound(w, r)
		return
	}
	w.Header().Del("X-Frame-Options")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src data:; connect-src 'none'; form-action 'none'; frame-ancestors 'self'; base-uri 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	base := "/api/v1/" + chi.URLParam(r, "gateSecret") + "/plugins/" + id + "/assets/"
	_ = pluginFrame.Execute(w, map[string]string{"Script": base + item.Manifest.UI.Script, "Style": func() string {
		if item.Manifest.UI.Style == "" {
			return ""
		}
		return base + item.Manifest.UI.Style
	}()})
}
func (h *PluginHandler) uiAsset(w http.ResponseWriter, r *http.Request) {
	path, err := h.service.UIAsset(r.Context(), chi.URLParam(r, "plugin_id"), chi.URLParam(r, "*"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		w.Header().Set("Content-Type", t)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}
func (h *PluginHandler) respond(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err == nil {
		WriteOK(w, r, data)
		return
	}
	status, code := http.StatusInternalServerError, app.ErrInternalError
	details := map[string]any(nil)
	var serviceErr *plugin.ServiceError
	switch {
	case errors.As(err, &serviceErr) && serviceErr.Code == "authorization_required":
		status, code, details = http.StatusConflict, "PLUGIN_AUTHORIZATION_REQUIRED", serviceErr.Details
	case errors.As(err, &serviceErr) && serviceErr.Status == http.StatusForbidden:
		status, code, details = http.StatusForbidden, "PLUGIN_ENTITLEMENT_DENIED", map[string]any{"reason": serviceErr.Code}
	case errors.Is(err, plugin.ErrAuthorizationNotFound):
		status, code = http.StatusNotFound, app.ErrNotFound
	case errors.Is(err, plugin.ErrDeviceAttemptNotFound):
		status, code = http.StatusGone, app.ErrBadRequest
	case errors.Is(err, plugin.ErrPluginNotFound):
		status, code = http.StatusNotFound, app.ErrNotFound
	case errors.Is(err, plugin.ErrPermissionApprovalRequired), errors.Is(err, plugin.ErrPluginEnabled):
		status, code = http.StatusConflict, app.ErrConflict
	case errors.Is(err, plugin.ErrDeveloperModeDisabled), errors.Is(err, plugin.ErrRiskAcknowledgementRequired):
		status, code = http.StatusForbidden, app.ErrForbidden
	case errors.Is(err, plugin.ErrDeveloperTokenInvalid):
		status, code = http.StatusGone, app.ErrBadRequest
	case errors.Is(err, plugin.ErrOfficialIDReserved):
		status, code = http.StatusConflict, app.ErrConflict
	case strings.Contains(err.Error(), "invalid"), strings.Contains(err.Error(), "mismatch"):
		status, code = http.StatusBadRequest, app.ErrBadRequest
	}
	WriteError(w, r, status, code, err.Error(), details)
}

var _ = fmt.Sprintf
