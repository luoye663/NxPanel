package plugin

import (
	"encoding/base64"
	"path/filepath"
	"strings"
)

// Official releases inject these values with -ldflags -X. Development builds
// intentionally leave them empty instead of falling back to trust-on-first-use.
var (
	OfficialPanelVersion      string
	OfficialMetadataURL       string
	OfficialTargetsURL        string
	OfficialServiceURL        string
	OfficialBootstrapRootB64  string
	OfficialAllowLoopbackHTTP string
)

func OfficialRepositoryConfig(dataDir string) (RepositoryConfig, error) {
	if strings.TrimSpace(OfficialMetadataURL) == "" || strings.TrimSpace(OfficialTargetsURL) == "" || strings.TrimSpace(OfficialBootstrapRootB64) == "" {
		return RepositoryConfig{}, ErrRepositoryNotConfigured
	}
	root, err := base64.StdEncoding.DecodeString(OfficialBootstrapRootB64)
	if err != nil {
		return RepositoryConfig{}, err
	}
	return RepositoryConfig{MetadataURL: OfficialMetadataURL, TargetsURL: OfficialTargetsURL, BootstrapRoot: root, CacheDir: filepath.Join(dataDir, "plugin-repository"), AllowHTTP: OfficialAllowLoopbackHTTP == "1"}, nil
}

func OfficialServiceConfig() (*OfficialServiceClient, error) {
	if strings.TrimSpace(OfficialServiceURL) == "" {
		return nil, ErrRepositoryNotConfigured
	}
	return NewOfficialServiceClient(OfficialServiceURL, OfficialAllowLoopbackHTTP == "1", nil)
}
