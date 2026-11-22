//go:build module_dev

package agent

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

func configuredModuleSource() moduleSource {
	return parseModuleSource(os.Getenv("NXPANEL_MODULE_TEST_FEED"), os.Getenv("NXPANEL_MODULE_TEST_KEYS"), inModuleContainer())
}

func parseModuleSource(feed, keys string, container bool) moduleSource {
	if feed == "" && keys == "" {
		return moduleSource{}
	}
	source := moduleSource{test: true}
	if feed == "" || keys == "" {
		source.err = fmt.Errorf("本地模块测试源须同时配置 NXPANEL_MODULE_TEST_FEED 和 NXPANEL_MODULE_TEST_KEYS")
		return source
	}
	address, err := url.Parse(feed)
	if err != nil || address.Scheme != "http" && address.Scheme != "https" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" || address.RawPath != "" ||
		!strings.HasSuffix(address.Path, "/"+nginxmodule.IndexName) || strings.ContainsAny(address.Path, "\\\t\r\n") || strings.Contains(address.Path, "/../") || strings.Contains(address.Path, "/./") {
		source.err = fmt.Errorf("测试源须为本地 HTTP(S) 的完整模块目录 URL")
		return source
	}
	host := address.Hostname()
	ip, ipErr := netip.ParseAddr(host)
	if ipErr != nil || ip.Zone() != "" || !ip.IsLoopback() {
		if !container || host != "module-feed" {
			source.err = fmt.Errorf("测试源只允许回环 IP 或 Docker 测试网络的 module-feed")
			return source
		}
	}
	if port := address.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			source.err = fmt.Errorf("测试源端口无效")
			return source
		}
	}
	source.keys, source.err = nginxmodule.Keys(keys, update.TrustKeys)
	if source.err == nil {
		_, source.err = nginxmodule.Keys(keys, nginxmodule.TrustKeys)
	}
	if source.err == nil {
		address.Path = strings.TrimSuffix(address.Path, nginxmodule.IndexName)
		source.base = address.String()
	}
	return source
}
