package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
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

type moduleFeed struct {
	entries  []nginxmodule.IndexEntry
	floor    nginxmodule.Floor
	fetched  time.Time
	failedAt time.Time
	fetchErr error
}

func (feed moduleFeed) entry(id string) (nginxmodule.IndexEntry, bool) {
	for _, entry := range feed.entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return nginxmodule.IndexEntry{}, false
}

type moduleIndexSnapshot struct {
	Index     []byte            `json:"index"`
	Signature []byte            `json:"signature"`
	Floor     nginxmodule.Floor `json:"floor"`
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
	test       bool
	base       string
	keys       map[string]ed25519.PublicKey
	err        error
	buildToken string
}

func (source moduleSource) stateDir() string {
	if source.test {
		return moduleTestStateDir
	}
	return moduleStateDir
}

func (source moduleSource) fetch(ctx context.Context, name string, limit int64, releases ...nginxmodule.Release) ([]byte, error) {
	if source.err != nil {
		return nil, source.err
	}
	if source.base == "" {
		return nil, fmt.Errorf("未配置官方模块服务地址")
	}
	address := source.base + name
	if len(releases) == 1 {
		release := releases[0]
		address = source.base + "repos/" + release.Repository + "/releases/download/" + release.Tag + "/" + name
	}
	return fetchModuleResource(ctx, moduleCatalogClient, address, "模块服务", limit)
}

var errModuleResourceNotFound = errors.New("模块资源不存在（HTTP 404）")

func fetchModuleResource(ctx context.Context, client *http.Client, address, label string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法访问%s", label)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s：%w", label, errModuleResourceNotFound)
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit {
		return nil, fmt.Errorf("%s资源不可用或超出大小限制（HTTP %d）", label, resp.StatusCode)
	}
	return readModuleBytes(resp.Body, limit)
}

var moduleCatalogClient = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{
		MaxResponseHeaderBytes: 64 << 10,
		DisableCompression:     true,
		MaxIdleConns:           4,
		IdleConnTimeout:        30 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (s *Server) cachedModuleFeed() moduleFeed {
	feed, err := s.moduleSource.savedIndex()
	if err != nil {
		return moduleFeed{}
	}
	return feed
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
	if !fresh && s.moduleFeed.fetchErr == nil && time.Since(s.moduleFeed.fetched) < 2*time.Minute {
		return s.moduleFeed, nil
	}
	defer func() {
		if loadErr != nil {
			s.moduleFeed.failedAt, s.moduleFeed.fetchErr = time.Now(), loadErr
		}
	}()
	if err := modulePrivateDir(source.stateDir(), true); err != nil {
		return moduleFeed{}, err
	}
	previous, err := source.readIndexSnapshot()
	if err != nil && !os.IsNotExist(err) {
		return moduleFeed{}, err
	}
	raw, err := source.fetch(ctx, nginxmodule.IndexName, nginxmodule.MaxEnvelope)
	if err != nil {
		return moduleFeed{}, err
	}
	signature, err := source.fetch(ctx, nginxmodule.IndexSigName, nginxmodule.MaxEnvelope)
	if err != nil {
		return moduleFeed{}, err
	}
	snapshot := moduleIndexSnapshot{Index: raw, Signature: signature}
	feed, err := source.verifyIndex(snapshot, previous.Floor)
	if err != nil {
		return moduleFeed{}, err
	}
	snapshot.Floor = feed.floor
	if previous.Floor != feed.floor || !bytes.Equal(previous.Signature, snapshot.Signature) {
		if err := writeModuleState(source.indexStatePath(), snapshot); err != nil {
			return moduleFeed{}, err
		}
	}
	feed.fetched = time.Now()
	s.moduleFeed = feed
	return s.moduleFeed, nil
}

func (source moduleSource) indexStatePath() string {
	return filepath.Join(source.stateDir(), "index-snapshot.json")
}

func (source moduleSource) savedIndex() (moduleFeed, error) {
	snapshot, err := source.readIndexSnapshot()
	if err != nil {
		return moduleFeed{}, err
	}
	return source.verifyIndex(snapshot, snapshot.Floor)
}

func (source moduleSource) readIndexSnapshot() (moduleIndexSnapshot, error) {
	var snapshot moduleIndexSnapshot
	if err := modulePrivateDir(source.stateDir(), false); err != nil {
		return snapshot, err
	}
	if err := readModuleState(source.indexStatePath(), 4*nginxmodule.MaxEnvelope, &snapshot); err != nil {
		return snapshot, err
	}
	var signature nginxmodule.IndexSignature
	if len(snapshot.Index) > nginxmodule.MaxEnvelope || len(snapshot.Signature) > nginxmodule.MaxEnvelope ||
		update.Decode(bytes.NewReader(snapshot.Signature), &signature) != nil || snapshot.Floor.Sequence == 0 ||
		snapshot.Floor.Sequence != signature.Sequence || snapshot.Floor.Digest != sha256Text(snapshot.Index) {
		return snapshot, fmt.Errorf("本地模块索引版本记录不可核验")
	}
	return snapshot, nil
}

func (source moduleSource) verifyIndex(snapshot moduleIndexSnapshot, floor nginxmodule.Floor) (moduleFeed, error) {
	keys, err := source.trustKeys()
	if err != nil {
		return moduleFeed{}, err
	}
	updateKeys, err := update.Keys(update.TrustKeys)
	if err != nil {
		return moduleFeed{}, err
	}
	entries, next, err := nginxmodule.VerifyIndex(snapshot.Index, snapshot.Signature, keys, updateKeys, floor, source.test)
	return moduleFeed{entries: entries, floor: next}, err
}

func moduleAssetMatches(body []byte, asset nginxmodule.Asset) bool {
	return int64(len(body)) == asset.Size && fmt.Sprintf("%x", sha256.Sum256(body)) == asset.SHA256
}
