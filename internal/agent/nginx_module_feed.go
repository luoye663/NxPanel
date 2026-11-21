package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

const moduleStateDir = "/var/lib/nxpanel-nginx-modules"
const moduleTestStateDir = moduleStateDir + "-test"

const moduleCatalogURL = "https://raw.githubusercontent.com/luoye663/NxPanel/main/" + nginxmodule.CatalogPath

type moduleFeed struct {
	catalog  nginxmodule.Catalog
	envelope []byte
	floor    nginxmodule.Floor
	fetched  time.Time
	failedAt time.Time
	fetchErr error
}

func (source moduleSource) trustKeys() (map[string]ed25519.PublicKey, error) {
	if source.err != nil {
		return nil, source.err
	}
	if source.test {
		return source.keys, nil
	}
	return nginxmodule.Keys(nginxmodule.TrustKeys, update.TrustKeys)
}

type moduleSource struct {
	test bool
	base string
	keys map[string]ed25519.PublicKey
	err  error
}

func (source moduleSource) stateDir() string {
	if source.test {
		return moduleTestStateDir
	}
	return moduleStateDir
}

var moduleHTTPClient = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{
		MaxResponseHeaderBytes: 64 << 10,
		DisableCompression:     true,
		MaxIdleConns:           4,
		IdleConnTimeout:        30 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 2 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" ||
			(req.URL.Host != "release-assets.githubusercontent.com" && req.URL.Host != "objects.githubusercontent.com") {
			return fmt.Errorf("非官方 GitHub Release 下载地址")
		}
		return nil
	},
}

func (source moduleSource) fetch(ctx context.Context, name string, limit int64, releases ...nginxmodule.Release) ([]byte, error) {
	address, client, label := moduleCatalogURL, moduleCatalogClient, "官方 GitHub 模块目录"
	if len(releases) == 1 {
		address, client, label = nginxmodule.ReleaseURL(releases[0], name), moduleHTTPClient, "官方 GitHub 模块产物"
	}
	if source.test {
		if source.err != nil {
			return nil, source.err
		}
		address, client, label = source.base+name, moduleCatalogClient, "本地模块测试源"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法访问%s", label)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit {
		return nil, fmt.Errorf("%s资源不可用或超出大小限制（HTTP %d）", label, resp.StatusCode)
	}
	return readModuleBytes(resp.Body, limit)
}

var moduleCatalogClient = &http.Client{
	Timeout:       12 * time.Second,
	Transport:     moduleHTTPClient.Transport,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (s *Server) cachedModuleFeed() moduleFeed {
	if time.Now().Before(s.moduleFeed.catalog.ExpiresAt) {
		return s.moduleFeed
	}
	return moduleFeed{}
}

func readModuleBytes(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件超出模块管理读取限制")
	}
	return data, nil
}

func modulePrivateDir(path string, create bool) error {
	if !create {
		if _, err := os.Lstat(path); err != nil {
			return err
		}
	}
	parent := filepath.Dir(path)
	if actual, err := trustedRuntimeDir(parent); err != nil || actual != parent {
		return fmt.Errorf("模块存储父目录不可信")
	}
	if create {
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || !runtimeOwned(info) || info.Mode().Perm() != 0700 {
		return fmt.Errorf("模块存储必须为私有目录: %s", path)
	}
	return nil
}

func readModuleState(path string, limit int64, target any) error {
	file, err := openRuntimeRegular(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := readModuleBytes(file, limit)
	if err != nil {
		return err
	}
	return update.Decode(bytes.NewReader(data), target)
}

func writeModuleState(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0600)
}

func (s *Server) loadModuleFeed(ctx context.Context, fresh bool) (_ moduleFeed, loadErr error) {
	source := s.moduleSource
	if source.err != nil {
		return moduleFeed{}, source.err
	}
	if !fresh && s.moduleFeed.fetchErr != nil && time.Since(s.moduleFeed.failedAt) < 10*time.Second {
		return moduleFeed{}, s.moduleFeed.fetchErr
	}
	if !fresh && s.moduleFeed.fetchErr == nil && time.Since(s.moduleFeed.fetched) < 2*time.Minute && time.Now().Before(s.moduleFeed.catalog.ExpiresAt) {
		return s.moduleFeed, nil
	}
	defer func() {
		if loadErr != nil {
			s.moduleFeed.failedAt, s.moduleFeed.fetchErr = time.Now(), loadErr
		}
	}()
	keys, err := source.trustKeys()
	if err != nil {
		return moduleFeed{}, err
	}
	verify := nginxmodule.Verify
	if source.test {
		verify = nginxmodule.VerifyTest
	}
	if err := modulePrivateDir(source.stateDir(), true); err != nil {
		return moduleFeed{}, err
	}
	var floor nginxmodule.Floor
	path := filepath.Join(source.stateDir(), "catalog-floor.json")
	if err := readModuleState(path, 1024, &floor); !os.IsNotExist(err) && (err != nil || floor.Sequence == 0 || !nginxmodule.ValidDigest(floor.Digest)) {
		return moduleFeed{}, fmt.Errorf("本地模块目录版本记录不可核验")
	}
	body, err := source.fetch(ctx, nginxmodule.CatalogName, nginxmodule.MaxEnvelope)
	if err != nil {
		return moduleFeed{}, err
	}
	now := time.Now()
	catalog, next, err := verify(body, keys, now, floor)
	if err != nil {
		return moduleFeed{}, err
	}
	if floor != next {
		if err := writeModuleState(path, next); err != nil {
			return moduleFeed{}, err
		}
	}
	s.moduleFeed = moduleFeed{catalog: catalog, envelope: body, floor: next, fetched: now}
	return s.moduleFeed, nil
}

func moduleAssetMatches(body []byte, asset nginxmodule.Asset) bool {
	return int64(len(body)) == asset.Size && fmt.Sprintf("%x", sha256.Sum256(body)) == asset.SHA256
}
