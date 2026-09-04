package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

const ManifestSchemaVersion = 1

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62})(?:\.[a-z0-9](?:[a-z0-9-]{0,62})){1,7}$`)

var allowedPermissions = map[string]struct{}{
	"panel.sites.read": {}, "plugin.kv": {}, "plugin.events": {}, "plugin.secrets": {},
	"http.fetch": {}, "scheduled_tasks.manage": {}, "operations.write": {},
	"notifications.show": {}, "native.waf.modsecurity": {},
}

var allowedProviders = map[string]struct{}{"waf.modsecurity.v1": {}}

type Manifest struct {
	SchemaVersion    int            `json:"schema_version"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Version          string         `json:"version"`
	Publisher        string         `json:"publisher"`
	PluginAPIVersion string         `json:"plugin_api_version"`
	PanelVersion     string         `json:"panel_version,omitempty"`
	Backend          string         `json:"backend"`
	UI               *UIEntrypoint  `json:"ui,omitempty"`
	Permissions      []string       `json:"permissions,omitempty"`
	NetworkDomains   []string       `json:"network_domains,omitempty"`
	Providers        []string       `json:"providers,omitempty"`
	Contributions    []Contribution `json:"contributions,omitempty"`
	Files            []FileDigest   `json:"files"`
}

type UIEntrypoint struct {
	Script string `json:"script"`
	Style  string `json:"style,omitempty"`
}

type Contribution struct {
	ID          string   `json:"id"`
	PluginID    string   `json:"plugin_id,omitempty"`
	Point       string   `json:"point"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Route       string   `json:"route,omitempty"`
	UIEntry     string   `json:"ui_entry,omitempty"`
	Renderer    string   `json:"renderer,omitempty"`
	RPCMethods  []string `json:"rpc_methods,omitempty"`
}

type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported manifest schema %d", m.SchemaVersion)
	}
	if !pluginIDPattern.MatchString(m.ID) || len(m.ID) > 255 {
		return errors.New("plugin id must be a reverse-domain identifier")
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 120 || strings.TrimSpace(m.Publisher) == "" {
		return errors.New("name and publisher are required")
	}
	if !semver.IsValid("v" + strings.TrimPrefix(m.Version, "v")) {
		return errors.New("version must be semantic versioning")
	}
	if m.PluginAPIVersion != "v1" {
		return fmt.Errorf("unsupported plugin API version %q", m.PluginAPIVersion)
	}
	if err := validatePackagePath(m.Backend); err != nil || !strings.HasSuffix(m.Backend, ".wasm") {
		return errors.New("backend must be a safe .wasm package path")
	}
	seenPermissions := make(map[string]bool)
	for _, permission := range m.Permissions {
		if _, ok := allowedPermissions[permission]; !ok {
			return fmt.Errorf("unknown permission %q", permission)
		}
		if seenPermissions[permission] {
			return fmt.Errorf("duplicate permission %q", permission)
		}
		seenPermissions[permission] = true
	}
	for _, domain := range m.NetworkDomains {
		if domain == "" || strings.ContainsAny(domain, "/:@ \\") || strings.HasPrefix(domain, ".") {
			return fmt.Errorf("invalid network domain %q", domain)
		}
	}
	seenProviders := make(map[string]bool)
	for _, provider := range m.Providers {
		if _, ok := allowedProviders[provider]; !ok {
			return fmt.Errorf("unknown provider %q", provider)
		}
		if seenProviders[provider] {
			return fmt.Errorf("duplicate provider %q", provider)
		}
		seenProviders[provider] = true
	}
	files := make(map[string]bool)
	for _, f := range m.Files {
		if err := validatePackagePath(f.Path); err != nil || f.Path == "manifest.json" {
			return fmt.Errorf("invalid file path %q", f.Path)
		}
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(f.SHA256) || f.Size < 0 {
			return fmt.Errorf("invalid digest for %q", f.Path)
		}
		if files[f.Path] {
			return fmt.Errorf("duplicate file %q", f.Path)
		}
		files[f.Path] = true
	}
	if !files[m.Backend] {
		return errors.New("backend is not declared in files")
	}
	if m.UI != nil {
		for _, entry := range []string{m.UI.Script, m.UI.Style} {
			if entry == "" {
				continue
			}
			if err := validatePackagePath(entry); err != nil || !files[entry] {
				return fmt.Errorf("UI entry %q is not a declared safe file", entry)
			}
		}
	}
	for i := range m.Contributions {
		contribution := &m.Contributions[i]
		if contribution.ID == "" || (contribution.Point != "global_page" && contribution.Point != "site_detail_tab") || contribution.Label == "" {
			return fmt.Errorf("invalid contribution %q", contribution.ID)
		}
		if contribution.UIEntry != "" {
			if m.UI == nil || contribution.UIEntry != m.UI.Script {
				return fmt.Errorf("contribution %q references an undeclared UI entry", contribution.ID)
			}
		}
		if contribution.Renderer != "" && contribution.Renderer != "iframe" && contribution.Renderer != "native_waf" {
			return fmt.Errorf("invalid renderer for contribution %q", contribution.ID)
		}
	}
	sort.Strings(m.Permissions)
	return nil
}

func validatePackagePath(name string) error {
	if name == "" || strings.ContainsRune(name, '\\') || path.IsAbs(name) || path.Clean(name) != name || name == "." || strings.HasPrefix(name, "../") {
		return errors.New("unsafe package path")
	}
	return nil
}
