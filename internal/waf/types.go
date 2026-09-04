// Package waf contains the validation and deterministic rendering primitives for
// the built-in ModSecurity provider. It deliberately has no process execution or
// filesystem access; privileged operations remain in internal/agent.
package waf

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	ProviderID = "waf.modsecurity.v1"
	ABIVersion = 1

	ModeDetectionOnly = "DetectionOnly"
	ModeOn            = "On"
)

var (
	safeIDPattern      = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	versionPattern     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	ruleIDPattern      = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)
	parameterPattern   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	providerPathPrefix = "providers/waf.modsecurity.v1"
)

var ErrUnsupportedProvider = errors.New("unsupported WAF provider or ABI")

// RuntimeFingerprint identifies the exact web-server build for which a native
// module was built. Digest is SHA-256 over the canonical JSON of all other fields.
type RuntimeFingerprint struct {
	ServerKind       string `json:"server_kind"`
	Version          string `json:"version"`
	ConfigureArgsSHA string `json:"configure_args_sha256"`
	BinarySHA        string `json:"binary_sha256"`
	GOOS             string `json:"goos"`
	GOARCH           string `json:"goarch"`
	Libc             string `json:"libc"`
	Digest           string `json:"digest"`
}

func (f RuntimeFingerprint) CanonicalDigest() (string, error) {
	copy := f
	copy.Digest = ""
	b, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (f RuntimeFingerprint) Validate() error {
	if f.ServerKind != "nginx" && f.ServerKind != "openresty" {
		return fmt.Errorf("unsupported server kind %q", f.ServerKind)
	}
	if f.Version == "" || f.GOOS == "" || f.GOARCH == "" || f.BinarySHA == "" || f.ConfigureArgsSHA == "" {
		return errors.New("runtime fingerprint is incomplete")
	}
	for name, value := range map[string]string{
		"binary_sha256": f.BinarySHA, "configure_args_sha256": f.ConfigureArgsSHA,
	} {
		if len(value) != 64 {
			return fmt.Errorf("%s must be a SHA-256 digest", name)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("%s must be a SHA-256 digest", name)
		}
	}
	expected, err := f.CanonicalDigest()
	if err != nil {
		return err
	}
	if !strings.EqualFold(expected, f.Digest) {
		return errors.New("runtime fingerprint digest mismatch")
	}
	return nil
}

func NewRuntimeFingerprint(kind, version, configureArgsSHA, binarySHA, goos, goarch, libc string) (RuntimeFingerprint, error) {
	f := RuntimeFingerprint{ServerKind: kind, Version: version, ConfigureArgsSHA: configureArgsSHA, BinarySHA: binarySHA, GOOS: goos, GOARCH: goarch, Libc: libc}
	digest, err := f.CanonicalDigest()
	if err != nil {
		return RuntimeFingerprint{}, err
	}
	f.Digest = digest
	return f, f.Validate()
}

func ValidateProvider(providerID string, abiVersion int) error {
	if providerID != ProviderID || abiVersion != ABIVersion {
		return fmt.Errorf("%w: provider=%q abi=%d", ErrUnsupportedProvider, providerID, abiVersion)
	}
	return nil
}

func ValidateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return errors.New("provider version must be semantic version")
	}
	return nil
}

func ValidateSiteID(siteID string) error {
	if !safeIDPattern.MatchString(siteID) {
		return errors.New("site_id contains unsupported characters")
	}
	return nil
}

func ProviderVersionDir(panelDir, version string) (string, error) {
	if err := ValidateVersion(version); err != nil {
		return "", err
	}
	return filepath.Join(panelDir, providerPathPrefix, "versions", version), nil
}

func SiteConfigPath(panelDir, siteID string) (string, error) {
	if err := ValidateSiteID(siteID); err != nil {
		return "", err
	}
	return filepath.Join(panelDir, "waf", "sites", siteID+".conf"), nil
}

type AuditConfig struct {
	Enabled    bool   `json:"enabled"`
	StorageDir string `json:"storage_dir,omitempty"`
}

type Exclusion struct {
	RuleID   string `json:"rule_id"`
	Path     string `json:"path,omitempty"`
	Param    string `json:"param,omitempty"`
	SourceIP string `json:"source_ip,omitempty"`
}

type SitePolicy struct {
	SiteID            string      `json:"site_id"`
	Enabled           bool        `json:"enabled"`
	Mode              string      `json:"mode"`
	ParanoiaLevel     int         `json:"paranoia_level"`
	InboundThreshold  int         `json:"inbound_threshold"`
	OutboundThreshold int         `json:"outbound_threshold"`
	ResponseStatus    int         `json:"response_status"`
	RequestBodyLimit  int64       `json:"request_body_limit"`
	NoFilesBodyLimit  int64       `json:"request_body_no_files_limit"`
	ResponseBodyLimit int64       `json:"response_body_limit"`
	Audit             AuditConfig `json:"audit"`
	Exclusions        []Exclusion `json:"exclusions,omitempty"`
	CRSSetupPath      string      `json:"crs_setup_path"`
	CRSRulesGlob      string      `json:"crs_rules_glob"`
	ModSecurityConf   string      `json:"modsecurity_conf_path"`
}

func DefaultSitePolicy(siteID string) SitePolicy {
	return SitePolicy{
		SiteID: siteID, Enabled: true, Mode: ModeDetectionOnly, ParanoiaLevel: 1,
		InboundThreshold: 5, OutboundThreshold: 4, ResponseStatus: 403,
		RequestBodyLimit: 13 * 1024 * 1024, NoFilesBodyLimit: 128 * 1024,
		ResponseBodyLimit: 512 * 1024,
	}
}

func (p SitePolicy) Validate(allowedRoots []string) error {
	if err := ValidateSiteID(p.SiteID); err != nil {
		return err
	}
	if p.Mode != ModeDetectionOnly && p.Mode != ModeOn {
		return errors.New("mode must be DetectionOnly or On")
	}
	if p.ParanoiaLevel < 1 || p.ParanoiaLevel > 4 {
		return errors.New("paranoia_level must be between 1 and 4")
	}
	if p.InboundThreshold < 1 || p.InboundThreshold > 100 || p.OutboundThreshold < 1 || p.OutboundThreshold > 100 {
		return errors.New("anomaly thresholds must be between 1 and 100")
	}
	if p.ResponseStatus < 400 || p.ResponseStatus > 599 {
		return errors.New("response_status must be between 400 and 599")
	}
	if p.RequestBodyLimit < 1024 || p.RequestBodyLimit > 1024*1024*1024 || p.NoFilesBodyLimit < 1024 || p.NoFilesBodyLimit > p.RequestBodyLimit {
		return errors.New("invalid request body limits")
	}
	if p.ResponseBodyLimit < 1024 || p.ResponseBodyLimit > 64*1024*1024 {
		return errors.New("invalid response body limit")
	}
	for name, path := range map[string]string{"modsecurity_conf_path": p.ModSecurityConf, "crs_setup_path": p.CRSSetupPath, "crs_rules_glob": p.CRSRulesGlob} {
		if err := validateConfigPath(path, allowedRoots, name == "crs_rules_glob"); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if p.Audit.Enabled {
		if err := validateConfigPath(p.Audit.StorageDir, allowedRoots, false); err != nil {
			return fmt.Errorf("audit storage_dir: %w", err)
		}
	}
	for i, exclusion := range p.Exclusions {
		if err := exclusion.Validate(); err != nil {
			return fmt.Errorf("exclusions[%d]: %w", i, err)
		}
	}
	return nil
}

func validateConfigPath(path string, roots []string, allowTrailingGlob bool) error {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00;{}'\\\t ") {
		return errors.New("must be a safe absolute path")
	}
	clean := filepath.Clean(path)
	if allowTrailingGlob && strings.HasSuffix(path, "/*.conf") {
		clean = filepath.Clean(strings.TrimSuffix(path, "/*.conf"))
	} else if strings.ContainsAny(path, "*?[]") {
		return errors.New("wildcards are not allowed")
	}
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return errors.New("path is outside provider roots")
}

func (e Exclusion) Validate() error {
	if !ruleIDPattern.MatchString(e.RuleID) {
		return errors.New("rule_id is invalid")
	}
	selectors := 0
	if e.Path != "" {
		selectors++
		if !strings.HasPrefix(e.Path, "/") || len(e.Path) > 512 || strings.ContainsAny(e.Path, "\r\n\x00\"") {
			return errors.New("path selector is invalid")
		}
	}
	if e.Param != "" {
		selectors++
		if !parameterPattern.MatchString(e.Param) {
			return errors.New("param selector is invalid")
		}
	}
	if e.SourceIP != "" {
		selectors++
		if !safeIPOrCIDR(e.SourceIP) {
			return errors.New("source_ip selector is invalid")
		}
	}
	if selectors > 1 {
		return errors.New("only one exclusion selector is allowed")
	}
	return nil
}

func safeIPOrCIDR(value string) bool {
	// Kept in render.go to avoid exposing the representation in the public model.
	return parseIPOrCIDR(value)
}

func sortedExclusions(in []Exclusion) []Exclusion {
	out := append([]Exclusion(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.RuleID+"\x00"+a.Path+"\x00"+a.Param+"\x00"+a.SourceIP < b.RuleID+"\x00"+b.Path+"\x00"+b.Param+"\x00"+b.SourceIP
	})
	return out
}
