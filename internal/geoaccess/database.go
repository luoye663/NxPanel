package geoaccess

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	maxMMDBBytes    = int64(64 * 1024 * 1024)
	maxArchiveBytes = int64(80 * 1024 * 1024)
	maxCacheBytes   = int64(128 * 1024 * 1024)
)

type countryCache struct {
	BuildEpoch int64               `json:"build_epoch"`
	Networks   map[string][]string `json:"networks"`
}

type mmdbCountryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"registered_country"`
}

func parseMMDB(path string) (*countryCache, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if info.Size() <= 0 || info.Size() > maxMMDBBytes {
		return nil, "", fmt.Errorf("GeoIP 数据库大小无效")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("打开 GeoLite2 数据库失败: %w", err)
	}
	defer db.Close()
	if !strings.Contains(strings.ToLower(db.Metadata.DatabaseType), "country") {
		return nil, "", fmt.Errorf("仅支持 GeoLite2/GeoIP Country MMDB")
	}
	cache := &countryCache{BuildEpoch: int64(db.Metadata.BuildEpoch), Networks: make(map[string][]string)}
	iter := db.Networks(maxminddb.SkipAliasedNetworks)
	for iter.Next() {
		var record mmdbCountryRecord
		network, err := iter.Network(&record)
		if err != nil {
			return nil, "", fmt.Errorf("读取 GeoIP 网络失败: %w", err)
		}
		country := strings.ToUpper(strings.TrimSpace(record.Country.ISOCode))
		if country == "" {
			country = strings.ToUpper(strings.TrimSpace(record.RegisteredCountry.ISOCode))
		}
		if len(country) != 2 {
			country = "ZZ"
		}
		cache.Networks[country] = append(cache.Networks[country], network.String())
	}
	if err := iter.Err(); err != nil {
		return nil, "", fmt.Errorf("遍历 GeoIP 数据库失败: %w", err)
	}
	if len(cache.Networks) == 0 {
		return nil, "", fmt.Errorf("GeoIP 数据库没有可用网络")
	}
	for country := range cache.Networks {
		aggregated, err := aggregateNetworks(cache.Networks[country])
		if err != nil {
			return nil, "", fmt.Errorf("合并 GeoIP 网络失败: %w", err)
		}
		cache.Networks[country] = aggregated
	}
	return cache, hex.EncodeToString(sum[:]), nil
}

func aggregateNetworks(values []string) ([]string, error) {
	buckets := make([]map[netip.Prefix]struct{}, 129)
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, err
		}
		prefix = prefix.Masked()
		bits := prefix.Bits()
		if buckets[bits] == nil {
			buckets[bits] = make(map[netip.Prefix]struct{})
		}
		buckets[bits][prefix] = struct{}{}
	}
	for bits := 128; bits > 0; bits-- {
		if len(buckets[bits]) < 2 {
			continue
		}
		parents := make(map[netip.Prefix]int, len(buckets[bits])/2)
		for prefix := range buckets[bits] {
			parent := netip.PrefixFrom(prefix.Addr(), bits-1).Masked()
			parents[parent]++
		}
		for prefix := range buckets[bits] {
			parent := netip.PrefixFrom(prefix.Addr(), bits-1).Masked()
			if parents[parent] != 2 {
				continue
			}
			delete(buckets[bits], prefix)
			if buckets[bits-1] == nil {
				buckets[bits-1] = make(map[netip.Prefix]struct{})
			}
			buckets[bits-1][parent] = struct{}{}
		}
	}
	result := make([]string, 0, len(values))
	for _, bucket := range buckets {
		for prefix := range bucket {
			result = append(result, prefix.String())
		}
	}
	sort.Strings(result)
	return result, nil
}

func writeCache(path string, cache *countryCache) error {
	raw, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	if int64(len(raw)) > maxCacheBytes {
		return fmt.Errorf("GeoIP 国家缓存超过 %d 字节", maxCacheBytes)
	}
	return writeAtomic(path, raw, 0600)
}

func readCache(path string) (*countryCache, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxCacheBytes {
		return nil, fmt.Errorf("GeoIP 国家缓存过大")
	}
	var cache countryCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil, err
	}
	if len(cache.Networks) == 0 {
		return nil, fmt.Errorf("GeoIP 国家缓存为空")
	}
	return &cache, nil
}

func extractMMDBFromTarGZ(r io.Reader, destination string) error {
	gz, err := gzip.NewReader(io.LimitReader(r, maxArchiveBytes+1))
	if err != nil {
		return fmt.Errorf("读取 GeoIP 压缩包失败: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 GeoIP 归档失败: %w", err)
		}
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(strings.ToLower(header.Name), ".mmdb") {
			continue
		}
		if found {
			return fmt.Errorf("GeoIP 归档包含多个 MMDB 文件")
		}
		if header.Size <= 0 || header.Size > maxMMDBBytes {
			return fmt.Errorf("归档中的 MMDB 大小无效")
		}
		f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(f, io.LimitReader(tr, header.Size))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != header.Size {
			return fmt.Errorf("归档中的 MMDB 内容不完整")
		}
		found = true
	}
	if !found {
		return fmt.Errorf("GeoIP 归档中未找到 MMDB")
	}
	return nil
}

func downloadDatabase(client *http.Client, accountID, licenseKey, destination string) error {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(licenseKey) == "" {
		return fmt.Errorf("MaxMind Account ID 和 License Key 不能为空")
	}
	req, err := http.NewRequest(http.MethodGet, "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(accountID, licenseKey)
	req.Header.Set("User-Agent", "nxpanel-geoip/1")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 GeoLite2 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MaxMind 返回 HTTP %d", resp.StatusCode)
	}
	return extractMMDBFromTarGZ(resp.Body, destination)
}

func installDatabaseFile(dataDir, sourcePath string) (dbPath, cachePath, checksum string, cache *countryCache, err error) {
	parsed, checksum, err := parseMMDB(sourcePath)
	if err != nil {
		return "", "", "", nil, err
	}
	versionDir := filepath.Join(dataDir, "geoip", "versions", checksum[:16])
	if err := os.MkdirAll(versionDir, 0700); err != nil {
		return "", "", "", nil, err
	}
	dbPath = filepath.Join(versionDir, "GeoLite2-Country.mmdb")
	cachePath = filepath.Join(versionDir, "countries.json")
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		raw, readErr := os.ReadFile(sourcePath)
		if readErr != nil {
			return "", "", "", nil, readErr
		}
		if err := writeAtomic(dbPath, raw, 0600); err != nil {
			return "", "", "", nil, err
		}
	}
	if err := writeCache(cachePath, parsed); err != nil {
		return "", "", "", nil, err
	}
	return dbPath, cachePath, checksum, parsed, nil
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func tempMMDB(dataDir string) (*os.File, error) {
	dir := filepath.Join(dataDir, "geoip", "tmp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "GeoLite2-*.mmdb")
}

func newDownloadClient() *http.Client { return &http.Client{Timeout: 2 * time.Minute} }
