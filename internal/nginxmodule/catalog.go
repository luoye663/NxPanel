package nginxmodule

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/luoye663/nxpanel/internal/update"
)

const (
	GeoIP2        = "geoip2"
	Filename      = "ngx_http_geoip2_module.so"
	Symbol        = "ngx_http_geoip2_module"
	Purpose       = "nxpanel-nginx-modules/v3"
	Product       = "nxpanel-nginx-modules"
	TestPurpose   = "nxpanel-nginx-modules-test/v3"
	TestProduct   = "nxpanel-nginx-modules-test"
	CatalogName   = "nxpanel-nginx-modules.json"
	CatalogPath   = "catalog/nginx-modules/" + CatalogName
	DynamicKind   = "nginx_dynamic_so"
	Protocol      = 3
	MaxModules    = 32
	MaxEnvelope   = 128 << 10
	MaxAsset      = 4 << 20
	MaxScript     = 128 << 10
	ScriptTimeout = 5 * time.Minute
	ActionTimeout = 6 * time.Minute
	MaxValidity   = 30 * 24 * time.Hour
)

var TrustKeys string

type Target struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Runtime        string `json:"runtime"`
	RuntimeVersion string `json:"runtime_version"`
	BinarySHA256   string `json:"binary_sha256"`
}

type Release struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

type Script struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Module struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Kind        string  `json:"kind"`
	Symbol      string  `json:"symbol,omitempty"`
	Release     Release `json:"release"`
	Install     Script  `json:"install"`
	Uninstall   Script  `json:"uninstall"`
}

func GeoIP2Module() Module {
	return Module{ID: GeoIP2, Name: "GeoIP2（HTTP）", Description: "使用 MaxMind MMDB 查询客户端国家或地区。数据库在 GeoIP 设置中单独管理。", Kind: DynamicKind, Symbol: Symbol}
}

type Asset struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Target  Target `json:"target"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type Catalog struct {
	Product   string    `json:"product"`
	Protocol  int       `json:"protocol"`
	Sequence  uint64    `json:"sequence"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Modules   []Module  `json:"modules"`
	Assets    []Asset   `json:"assets"`
}

type Envelope struct {
	Catalog    []byte            `json:"catalog"`
	Signatures map[string]string `json:"signatures"`
}

type Floor struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
	symbolPattern  = regexp.MustCompile(`^ngx_[a-zA-Z0-9_]{1,100}_module$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?/[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)
	tagPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
	osPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,23}:[a-z0-9][a-z0-9._-]{0,23}$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+~_-]{0,63}$`)
)

func ValidDigest(value string) bool {
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == sha256.Size && hex.EncodeToString(digest) == value
}

func ValidID(value string) bool                   { return idPattern.MatchString(value) }
func ValidSymbol(value string) bool               { return symbolPattern.MatchString(value) }
func AssetName(id, digest string) string          { return id + "-" + digest + ".so" }
func ScriptName(id, action, digest string) string { return id + "-" + action + "-" + digest + ".sh" }
func (script Script) valid(id, action string) bool {
	return script.Size > 0 && script.Size <= MaxScript && ValidDigest(script.SHA256) && script.Name == ScriptName(id, action, script.SHA256)
}
func ReleaseURL(release Release, name string) string {
	return "https://github.com/" + release.Repository + "/releases/download/" + release.Tag + "/" + name
}

func validText(value string, limit int) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, func(char rune) bool {
		return unicode.IsControl(char) || unicode.Is(unicode.Cf, char)
	}) == -1
}

func Keys(value, updateKeys string) (map[string]ed25519.PublicKey, error) {
	keys, err := update.Keys(value)
	if err != nil || len(keys) == 0 {
		return nil, fmt.Errorf("未配置可信模块发布公钥")
	}
	other, err := update.Keys(updateKeys)
	if err != nil {
		return nil, err
	}
	for _, moduleKey := range keys {
		for _, updateKey := range other {
			if bytes.Equal(moduleKey, updateKey) {
				return nil, fmt.Errorf("模块发布不得复用面板更新密钥")
			}
		}
	}
	return keys, nil
}

func Verify(body []byte, keys map[string]ed25519.PublicKey, now time.Time, floor Floor) (Catalog, Floor, error) {
	return verify(body, keys, now, floor, false, false)
}

func VerifyTest(body []byte, keys map[string]ed25519.PublicKey, now time.Time, floor Floor) (Catalog, Floor, error) {
	return verify(body, keys, now, floor, true, false)
}

func VerifyInstalled(body []byte, keys map[string]ed25519.PublicKey, now time.Time, test bool) (Catalog, Floor, error) {
	return verify(body, keys, now, Floor{}, test, true)
}

func verify(body []byte, keys map[string]ed25519.PublicKey, now time.Time, floor Floor, test, installed bool) (Catalog, Floor, error) {
	var envelope Envelope
	var catalog Catalog
	if len(body) > MaxEnvelope || update.Decode(bytes.NewReader(body), &envelope) != nil {
		return catalog, floor, fmt.Errorf("无效的模块目录封包")
	}
	signatures, err := json.Marshal(envelope.Signatures)
	purpose := Purpose
	if test {
		purpose = TestPurpose
	}
	if err != nil || update.VerifySignature(envelope.Catalog, signatures, keys, purpose) != nil {
		return catalog, floor, fmt.Errorf("模块目录签名不可信")
	}
	if err := update.Decode(bytes.NewReader(envelope.Catalog), &catalog); err != nil {
		return catalog, floor, fmt.Errorf("无效的签名模块目录: %w", err)
	}
	if installed && !catalog.IssuedAt.After(now) {
		now = catalog.IssuedAt
	}
	if err := catalog.validate(now, test); err != nil {
		return catalog, floor, err
	}
	next := Floor{Sequence: catalog.Sequence, Digest: fmt.Sprintf("%x", sha256.Sum256(envelope.Catalog))}
	if next.Sequence < floor.Sequence || next.Sequence == floor.Sequence && next.Digest != floor.Digest {
		return catalog, floor, fmt.Errorf("模块目录版本回退或同序号内容变化")
	}
	return catalog, next, nil
}

func (catalog Catalog) Module(id string) (Module, bool) {
	for _, module := range catalog.Modules {
		if module.ID == id {
			return module, true
		}
	}
	return Module{}, false
}

func (catalog Catalog) Validate(now time.Time) error {
	return catalog.validate(now, false)
}

func (catalog Catalog) ValidateTest(now time.Time) error {
	return catalog.validate(now, true)
}

func (catalog Catalog) validate(now time.Time, test bool) error {
	product := Product
	if test {
		product = TestProduct
	}
	if catalog.Product != product || catalog.Protocol != Protocol || catalog.Sequence == 0 || catalog.Modules == nil || len(catalog.Modules) > MaxModules || len(catalog.Assets) > 128 {
		return fmt.Errorf("不兼容的模块目录")
	}
	if catalog.IssuedAt.IsZero() || catalog.IssuedAt.After(now.Add(5*time.Minute)) || !catalog.ExpiresAt.After(now) ||
		!catalog.ExpiresAt.After(catalog.IssuedAt) || catalog.ExpiresAt.Sub(catalog.IssuedAt) > MaxValidity {
		return fmt.Errorf("模块目录已过期或有效时间无效")
	}
	modules, symbols := map[string]bool{}, map[string]bool{}
	for _, module := range catalog.Modules {
		if !ValidID(module.ID) || modules[module.ID] || !validText(module.Name, 128) || !validText(module.Description, 768) || !ValidID(module.Kind) ||
			module.Kind == DynamicKind && (!ValidSymbol(module.Symbol) || symbols[module.Symbol] || !module.Install.valid(module.ID, "install") || !module.Uninstall.valid(module.ID, "uninstall")) ||
			module.ID == GeoIP2 && (module.Kind != DynamicKind || module.Symbol != Symbol) || module.ID != GeoIP2 && module.Symbol == Symbol ||
			!test && (!repoPattern.MatchString(module.Release.Repository) || !tagPattern.MatchString(module.Release.Tag)) || test && module.Release != (Release{}) {
			return fmt.Errorf("无效或重复的模块目录条目")
		}
		modules[module.ID] = true
		if module.Kind == DynamicKind {
			symbols[module.Symbol] = true
		}
	}
	type matchKey struct {
		ID     string
		Target Target
	}
	seen := map[matchKey]bool{}
	versions := map[string]string{}
	for _, asset := range catalog.Assets {
		target := asset.Target
		key := matchKey{asset.Module, target}
		if !modules[asset.Module] || !update.ValidVersion(asset.Version) || versions[asset.Module] != "" && versions[asset.Module] != asset.Version || !osPattern.MatchString(target.OS) ||
			target.Arch != "amd64" && target.Arch != "arm64" || target.Runtime != "nginx" && target.Runtime != "openresty" ||
			!versionPattern.MatchString(target.RuntimeVersion) || !ValidDigest(target.BinarySHA256) || seen[key] ||
			!ValidDigest(asset.SHA256) || asset.Name != AssetName(asset.Module, asset.SHA256) || asset.Size <= 0 || asset.Size > MaxAsset {
			return fmt.Errorf("无效或重复的模块构建目标")
		}
		seen[key] = true
		versions[asset.Module] = asset.Version
	}
	return nil
}

func (catalog Catalog) Match(id string, target Target) (Asset, bool) {
	for _, asset := range catalog.Assets {
		if asset.Module == id && asset.Target == target {
			return asset, true
		}
	}
	return Asset{}, false
}

type Listing struct {
	Modules     []Status `json:"modules"`
	FeedState   string   `json:"feed_state"`
	FeedMessage string   `json:"feed_message,omitempty"`
	TestFeed    bool     `json:"test_feed,omitempty"`
	Busy        bool     `json:"busy,omitempty"`
}

type Status struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Description           string `json:"description,omitempty"`
	TestFeed              bool   `json:"test_feed,omitempty"`
	State                 string `json:"state"`
	Present               bool   `json:"present"`
	Loaded                bool   `json:"loaded"`
	Configured            bool   `json:"configured"`
	Runtime               string `json:"runtime,omitempty"`
	Version               string `json:"version,omitempty"`
	Reason                string `json:"reason,omitempty"`
	Action                string `json:"action,omitempty"`
	Confirmation          string `json:"confirmation,omitempty"`
	UninstallConfirmation string `json:"uninstall_confirmation,omitempty"`
}

type InstallRequest struct {
	ID           string `json:"id"`
	Confirmation string `json:"confirmation"`
}

type Result struct {
	State        string `json:"state"`
	Message      string `json:"message,omitempty"`
	ScriptSHA256 string `json:"script_sha256,omitempty"`
	Version      string `json:"version,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}
