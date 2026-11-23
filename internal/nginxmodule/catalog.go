package nginxmodule

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
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
	GeoIP2          = "geoip2"
	Filename        = "ngx_http_geoip2_module.so"
	Symbol          = "ngx_http_geoip2_module"
	IndexName       = "modules.json"
	IndexSigName    = "modules.sig.json"
	IndexPath       = "catalog/nginx-modules/"
	ManifestName    = "module-release.json"
	ManifestSig     = "module-release.sig.json"
	IndexPurpose    = "nxpanel-nginx-module-index/v1"
	ReleasePurpose  = "nxpanel-nginx-module-release/v1"
	DynamicKind     = "nginx_dynamic_so"
	Protocol        = 1
	RangeProtocol   = 2
	MaxModules      = 32
	MaxEnvelope     = 128 << 10
	MaxAsset        = 4 << 20
	MaxScript       = 128 << 10
	ScriptTimeout   = 5 * time.Minute
	ActionTimeout   = 6 * time.Minute
	ReleasePageSize = 20
	MaxReleasePage  = 100
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
	Module        string          `json:"module"`
	Version       string          `json:"version"`
	Target        Target          `json:"target"`
	Compatibility []Compatibility `json:"compatibility,omitempty"`
	Name          string          `json:"name"`
	Size          int64           `json:"size"`
	SHA256        string          `json:"sha256"`
}

type Manifest struct {
	Protocol int          `json:"protocol"`
	Module   Module       `json:"module"`
	Assets   []Asset      `json:"assets"`
	Build    *BuildRecipe `json:"build,omitempty"`
}

type IndexEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Repository  string `json:"repository"`
	Symbol      string `json:"symbol"`
	Keys        string `json:"keys"`
}

type Floor struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

type IndexSignature struct {
	Sequence   uint64            `json:"sequence"`
	Signatures map[string]string `json:"signatures"`
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
func ValidRepository(value string) bool           { return repoPattern.MatchString(value) }
func ValidTag(value string) bool                  { return tagPattern.MatchString(value) }
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

func sameKey(first, second map[string]ed25519.PublicKey) bool {
	for _, key := range first {
		for _, other := range second {
			if bytes.Equal(key, other) {
				return true
			}
		}
	}
	return false
}

func (entry IndexEntry) DisplayModule() Module {
	return Module{ID: entry.ID, Name: entry.Name, Description: entry.Description, Kind: DynamicKind, Symbol: entry.Symbol, Release: Release{Repository: entry.Repository}}
}

func (entry IndexEntry) PublicKeys() (map[string]ed25519.PublicKey, error) {
	keys, err := update.Keys(entry.Keys)
	if err != nil || len(keys) == 0 || len(keys) > 8 {
		return nil, fmt.Errorf("无效的模块发布公钥")
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if seen[string(key)] {
			return nil, fmt.Errorf("重复的模块发布公钥")
		}
		seen[string(key)] = true
	}
	return keys, nil
}

func validIdentity(id, name, description, symbol string) bool {
	return ValidID(id) && validText(name, 128) && validText(description, 768) && ValidSymbol(symbol) && (id == GeoIP2) == (symbol == Symbol)
}

func IndexPayload(raw []byte, sequence uint64) []byte {
	return fmt.Appendf(nil, "%d\n%x", sequence, sha256.Sum256(raw))
}

func modulePurpose(purpose string, test bool) string {
	if test {
		return purpose + "/test"
	}
	return purpose
}

func VerifyIndex(raw, signature []byte, roots, updateKeys map[string]ed25519.PublicKey, floor Floor, test bool) ([]IndexEntry, Floor, error) {
	var signed IndexSignature
	if len(raw) > MaxEnvelope || len(signature) > MaxEnvelope || update.Decode(bytes.NewReader(signature), &signed) != nil || signed.Sequence == 0 {
		return nil, floor, fmt.Errorf("无效的模块索引签名格式")
	}
	signatures, err := json.Marshal(signed.Signatures)
	if err != nil || update.VerifySignature(IndexPayload(raw, signed.Sequence), signatures, roots, modulePurpose(IndexPurpose, test)) != nil {
		return nil, floor, fmt.Errorf("模块索引签名不可信")
	}
	next := Floor{Sequence: signed.Sequence, Digest: fmt.Sprintf("%x", sha256.Sum256(raw))}
	if next.Sequence < floor.Sequence || next.Sequence == floor.Sequence && next.Digest != floor.Digest {
		return nil, floor, fmt.Errorf("模块索引版本回退或同序号内容变化")
	}
	var entries []IndexEntry
	if update.Decode(bytes.NewReader(raw), &entries) != nil || entries == nil || len(entries) > MaxModules {
		return nil, floor, fmt.Errorf("无效的模块索引")
	}
	ids, repos, symbols := map[string]bool{}, map[string]bool{}, map[string]bool{}
	used := make(map[string]ed25519.PublicKey)
	for _, entry := range entries {
		repo := strings.ToLower(entry.Repository)
		keys, err := entry.PublicKeys()
		if !validIdentity(entry.ID, entry.Name, entry.Description, entry.Symbol) || !ValidRepository(entry.Repository) || ids[entry.ID] || repos[repo] || symbols[entry.Symbol] ||
			err != nil || sameKey(keys, roots) || sameKey(keys, updateKeys) || sameKey(keys, used) {
			return nil, floor, fmt.Errorf("无效或重复的模块索引条目: %s", entry.ID)
		}
		ids[entry.ID], repos[repo], symbols[entry.Symbol] = true, true, true
		for _, key := range keys {
			used[string(key)] = key
		}
	}
	return entries, next, nil
}

func Sign(payload []byte, keys map[string]ed25519.PrivateKey, purpose string, test bool) map[string]string {
	result := make(map[string]string, len(keys))
	message := update.SignatureMessage(modulePurpose(purpose, test), payload)
	for id, key := range keys {
		result[id] = base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))
	}
	return result
}

func VerifyManifest(raw, signature []byte, entry IndexEntry, tag string, test bool) (Manifest, error) {
	var manifest Manifest
	keys, err := entry.PublicKeys()
	if err != nil || update.VerifySignature(raw, signature, keys, modulePurpose(ReleasePurpose, test)) != nil {
		return manifest, fmt.Errorf("模块 Release 签名不可信")
	}
	if err := update.Decode(bytes.NewReader(raw), &manifest); err != nil {
		return manifest, fmt.Errorf("无效的模块 Release 清单")
	}
	if err := manifest.Validate(); err != nil {
		return manifest, err
	}
	module := manifest.Module
	if module.ID != entry.ID || module.Symbol != entry.Symbol || !strings.EqualFold(module.Release.Repository, entry.Repository) || module.Release.Tag != tag {
		return manifest, fmt.Errorf("签名清单与登记的模块、仓库或版本不符")
	}
	return manifest, nil
}

func (manifest Manifest) Validate() error {
	module := manifest.Module
	protocol := ManifestProtocol(manifest.Assets)
	if manifest.Build != nil {
		protocol = BuildProtocol
		if !manifest.Build.valid(module.ID) || !update.ValidVersion(module.Release.Tag) {
			return fmt.Errorf("无效的模块源码构建配方")
		}
	}
	if manifest.Protocol != protocol {
		return fmt.Errorf("不支持的模块 Release 清单协议: %d", manifest.Protocol)
	}
	if !validIdentity(module.ID, module.Name, module.Description, module.Symbol) || module.Kind != DynamicKind ||
		!ValidRepository(module.Release.Repository) || !ValidTag(module.Release.Tag) || !module.Install.valid(module.ID, "install") ||
		!module.Uninstall.valid(module.ID, "uninstall") || len(manifest.Assets) == 0 && manifest.Build == nil || len(manifest.Assets) > 128 {
		return fmt.Errorf("无效的模块 Release 清单")
	}
	seen := make(map[Target]bool)
	for _, asset := range manifest.Assets {
		target := asset.Target
		if asset.Module != module.ID || !update.ValidVersion(asset.Version) || asset.Version != manifest.Assets[0].Version || !osPattern.MatchString(target.OS) ||
			!validPlatform(target.Arch, target.Runtime) ||
			!versionPattern.MatchString(target.RuntimeVersion) || !ValidDigest(target.BinarySHA256) || asset.Compatibility == nil && seen[target] ||
			!ValidDigest(asset.SHA256) || asset.Name != AssetName(asset.Module, asset.SHA256) || asset.Size <= 0 || asset.Size > MaxAsset {
			return fmt.Errorf("无效或重复的模块构建目标")
		}
		if err := validateCompatibility(asset.Compatibility); err != nil {
			return err
		}
		if asset.Compatibility == nil {
			seen[target] = true
		}
	}
	return nil
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
	Repository            string `json:"repository,omitempty"`
	TestFeed              bool   `json:"test_feed,omitempty"`
	State                 string `json:"state"`
	Present               bool   `json:"present"`
	Loaded                bool   `json:"loaded"`
	Configured            bool   `json:"configured"`
	Runtime               string `json:"runtime,omitempty"`
	Version               string `json:"version,omitempty"`
	Reason                string `json:"reason,omitempty"`
	Action                string `json:"action,omitempty"`
	UninstallConfirmation string `json:"uninstall_confirmation,omitempty"`
}

type InstallRequest struct {
	ID           string `json:"id"`
	Confirmation string `json:"confirmation"`
	Tag          string `json:"tag,omitempty"`
}

func (request InstallRequest) Valid(uninstall bool) bool {
	return ValidID(request.ID) && ValidDigest(request.Confirmation) && (uninstall && request.Tag == "" || !uninstall && ValidTag(request.Tag))
}

type ReleasePage struct {
	Tags     []string `json:"tags"`
	Latest   string   `json:"latest,omitempty"`
	NextPage int      `json:"next_page,omitempty"`
}

type Preflight struct {
	Tag          string       `json:"tag"`
	Version      string       `json:"version,omitempty"`
	Runtime      string       `json:"runtime,omitempty"`
	Confirmation string       `json:"confirmation,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	Build        *BuildStatus `json:"build,omitempty"`
}

type Result struct {
	State        string `json:"state"`
	Message      string `json:"message,omitempty"`
	ScriptSHA256 string `json:"script_sha256,omitempty"`
	Version      string `json:"version,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}
