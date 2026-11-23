package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

func officialModuleSource() moduleSource {
	base := nginxmodule.StoreURL
	if override := os.Getenv("NXPANEL_MODULE_STORE_URL"); override != "" {
		base = override
	}
	if base == "" {
		return moduleSource{}
	}
	address, err := url.Parse(base)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" || address.RawPath != "" || address.Path != "" && address.Path != "/" {
		return moduleSource{err: fmt.Errorf("模块服务地址必须是 HTTPS 源站地址")}
	}
	return moduleSource{base: strings.TrimSuffix(address.String(), "/") + nginxmodule.StorePath, buildToken: os.Getenv("NXPANEL_MODULE_BUILD_TOKEN")}
}

func observedBuildRequest(ctx context.Context, instance moduleRuntime, target nginxmodule.Target, manifest nginxmodule.Manifest, raw []byte) nginxmodule.BuildRequest {
	request := nginxmodule.BuildRequest{ID: manifest.Module.ID, Tag: manifest.Module.Release.Tag, ManifestSHA256: sha256Text(raw), Target: target}
	osID, _, _ := strings.Cut(target.OS, ":")
	var binary string
	switch target.Runtime {
	case "nginx":
		binary = "/usr/sbin/nginx"
	case "openresty":
		binary = "/usr/local/openresty/nginx/sbin/nginx"
	}
	if canonicalRuntimePath(instance.bin) != binary {
		return request
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var command *exec.Cmd
	switch osID {
	case "debian", "ubuntu":
		command = exec.CommandContext(ctx, "/usr/bin/dpkg-query", "-W", "-f=${db:Status-Status}\t${Version}", target.Runtime)
	case "rocky":
		command = exec.CommandContext(ctx, "/usr/bin/rpm", "-qf", "--qf", "%{EPOCHNUM}:%{VERSION}-%{RELEASE}", binary)
	default:
		return request
	}
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	if data, err := command.Output(); err == nil && len(data) < 256 {
		version := strings.TrimSpace(string(data))
		if osID != "rocky" {
			version, _ = strings.CutPrefix(version, "installed\t")
		}
		request.PackageVersion = version
		if !request.Valid() {
			request.PackageVersion = ""
		}
	}
	return request
}

func (source moduleSource) buildStatus(ctx context.Context, request nginxmodule.BuildRequest, submit bool) (nginxmodule.BuildStatus, error) {
	var status nginxmodule.BuildStatus
	if source.test || source.base == "" || !request.Valid() || nginxmodule.BuildTrustKeys == "" {
		return nginxmodule.BuildStatus{State: "unsupported", Message: "当前 Agent 未配置可信的按需构建服务"}, nil
	}
	method, endpoint := http.MethodPost, "builds/lookup"
	if submit {
		endpoint = "builds"
		if source.buildToken == "" {
			return status, fmt.Errorf("此面板没有构建令牌")
		}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return status, err
	}
	req, err := http.NewRequestWithContext(ctx, method, source.base+endpoint, bytes.NewReader(body))
	if err != nil {
		return status, err
	}
	req.Header.Set("Content-Type", "application/json")
	if submit {
		req.Header.Set("Authorization", "Bearer "+source.buildToken)
	}
	resp, err := moduleCatalogClient.Do(req)
	if err != nil {
		return status, fmt.Errorf("模块构建服务不可用")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return status, fmt.Errorf("模块构建请求未受理（HTTP %d）", resp.StatusCode)
	}
	data, err := readModuleBytes(resp.Body, 4096)
	if err != nil || update.Decode(bytes.NewReader(data), &status) != nil {
		return status, fmt.Errorf("模块构建状态响应无效")
	}
	switch status.State {
	case "missing", "queued", "building", "ready", "failed", "unsupported":
	default:
		return status, fmt.Errorf("模块构建状态未知")
	}
	status.CanSubmit = (status.State == "missing" || status.State == "failed") && source.buildToken != ""
	return status, nil
}

func (source moduleSource) getBuild(ctx context.Context, request nginxmodule.BuildRequest, manifest nginxmodule.Manifest, raw []byte, entry nginxmodule.IndexEntry) (nginxmodule.BuildResult, nginxmodule.SignedBuild, error) {
	var result nginxmodule.BuildResult
	var signed nginxmodule.SignedBuild
	data, err := source.fetch(ctx, "builds/"+request.Key()+"/result", nginxmodule.MaxEnvelope)
	if err != nil {
		return result, signed, err
	}
	if update.Decode(bytes.NewReader(data), &signed) != nil {
		return result, signed, fmt.Errorf("构建结果响应无效")
	}
	publishers, err := entry.PublicKeys()
	if err != nil {
		return result, signed, err
	}
	result, err = signed.Verify(manifest, raw, nginxmodule.BuildTrustKeys, nginxmodule.TrustKeys, update.TrustKeys, publishers)
	if err == nil && result.Request != request {
		err = fmt.Errorf("构建结果与请求的运行时不匹配")
	}
	return result, signed, err
}
