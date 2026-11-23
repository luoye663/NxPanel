package nginxmodule

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/luoye663/nxpanel/internal/trust"
	"github.com/luoye663/nxpanel/internal/update"
)

const (
	StorePath     = "/v1/"
	BuildPurpose  = "nxpanel-nginx-module-build/v1"
	BuildProtocol = 3
	MaxSource     = 16 << 20
)

var (
	StoreURL       string
	packageVersion = regexp.MustCompile(`^[0-9][0-9A-Za-z.+:~_-]{0,126}$`)
)

type BuildRecipe struct {
	Source Script `json:"source"`
}

func SourceName(id, digest string) string { return id + "-source-" + digest + ".tar.gz" }

func (recipe BuildRecipe) valid(id string) bool {
	return ValidDigest(recipe.Source.SHA256) && recipe.Source.Name == SourceName(id, recipe.Source.SHA256) && recipe.Source.Size > 0 && recipe.Source.Size <= MaxSource
}

type BuildRequest struct {
	ID             string `json:"id"`
	Tag            string `json:"tag"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Target         Target `json:"target"`
	PackageVersion string `json:"package_version,omitempty"`
}

func (request BuildRequest) Valid() bool {
	target := request.Target
	return ValidID(request.ID) && ValidTag(request.Tag) && ValidDigest(request.ManifestSHA256) &&
		osPattern.MatchString(target.OS) && validPlatform(target.Arch, target.Runtime) && validNumericVersion(target.RuntimeVersion) &&
		ValidDigest(target.BinarySHA256) && (request.PackageVersion == "" || packageVersion.MatchString(request.PackageVersion))
}

func (request BuildRequest) Key() string {
	data, _ := json.Marshal(request)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

type BuildStatus struct {
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	CanSubmit bool   `json:"can_submit,omitempty"`
}

type BuildResult struct {
	Request BuildRequest `json:"request"`
	Asset   Asset        `json:"asset"`
}

type SignedBuild struct {
	Result    json.RawMessage `json:"result"`
	Signature json.RawMessage `json:"signature"`
}

func (signed SignedBuild) Verify(manifest Manifest, raw []byte, keys string) (BuildResult, error) {
	var result BuildResult
	buildKeys, err := trust.Keys(keys)
	if err != nil || update.VerifySignature(signed.Result, signed.Signature, buildKeys, BuildPurpose) != nil {
		return result, fmt.Errorf("构建服务签名不可信")
	}
	if update.Decode(bytes.NewReader(signed.Result), &result) != nil || !result.Request.Valid() || manifest.Build == nil ||
		result.Request.ManifestSHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || result.Request.ID != manifest.Module.ID || result.Request.Tag != manifest.Module.Release.Tag {
		return result, fmt.Errorf("构建结果与模块 Release 不匹配")
	}
	asset := result.Asset
	if asset.Module != manifest.Module.ID || asset.Version != manifest.Module.Release.Tag || asset.Target != result.Request.Target || asset.Compatibility != nil ||
		!ValidDigest(asset.SHA256) || asset.Name != AssetName(asset.Module, asset.SHA256) || asset.Size <= 0 || asset.Size > MaxAsset {
		return result, fmt.Errorf("构建产物元数据无效")
	}
	return result, nil
}
