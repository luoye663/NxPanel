package sites

import (
	"github.com/luoye663/nxpanel/internal/db/repo"
	nginx "github.com/luoye663/nxpanel/internal/nginx"
)

type Binding = repo.Binding

type CreateSiteRequest struct {
	Bindings          []Binding `json:"bindings"`
	RootPath          string    `json:"root_path"`
	IndexFiles        string    `json:"index_files"`
	AccessLogEnabled  bool      `json:"access_log_enabled"`
	CreateRoot        bool      `json:"create_root"`
	CreateIndex       bool      `json:"create_index"`
	EnableAfterCreate bool      `json:"enable_after_create"`
}

type UpdateSiteRequest struct {
	Bindings         []Binding `json:"bindings"`
	HTTPSPort        int       `json:"https_port"`
	RootPath         string    `json:"root_path"`
	IndexFiles       string    `json:"index_files"`
	AccessLogEnabled bool      `json:"access_log_enabled"`
	ExpectedFileHash string    `json:"expected_file_hash"`
}

type UpdateSiteDocumentRequest struct {
	IndexFiles         []string `json:"index_files"`
	AutoindexEnabled   bool     `json:"autoindex_enabled"`
	AutoindexExactSize bool     `json:"autoindex_exact_size"`
	AutoindexLocaltime bool     `json:"autoindex_localtime"`
	AutoindexFormat    string   `json:"autoindex_format"`
	ErrorPage404       string   `json:"error_page_404"`
	ErrorPage403       string   `json:"error_page_403"`
	ExpectedFileHash   string   `json:"expected_file_hash"`
}

type EnableSiteRequest struct{}

type DisableSiteRequest struct{}

type DeleteSiteRequest struct {
	DeleteRoot           bool   `json:"delete_root"`
	DeleteLogs           bool   `json:"delete_logs"`
	DeleteSSLFiles       bool   `json:"delete_ssl_files"`
	ConfirmPrimaryDomain string `json:"confirm_primary_domain,omitempty"`
}

type SiteListItem struct {
	ID            string    `json:"id"`
	PrimaryDomain string    `json:"primary_domain"`
	Domains       []string  `json:"domains"`
	Bindings      []Binding `json:"bindings"`
	Status        string    `json:"status"`
	RootPath      string    `json:"root_path"`
	AccessLogPath string    `json:"access_log_path"`
	ErrorLogPath  string    `json:"error_log_path"`
	UpdatedAt     string    `json:"updated_at"`
	SSLEnabled    bool      `json:"ssl_enabled"`
	ProxyEnabled  bool      `json:"proxy_enabled"`
}

type SiteDetailResponse struct {
	ID                 string             `json:"id"`
	PrimaryDomain      string             `json:"primary_domain"`
	Domains            []string           `json:"domains"`
	Bindings           []Binding          `json:"bindings"`
	Status             string             `json:"status"`
	HTTPPort           int                `json:"http_port"`
	HTTPSPort          int                `json:"https_port"`
	ConfigPath         string             `json:"config_path"`
	RootPath           string             `json:"root_path"`
	IndexFiles         string             `json:"index_files"`
	IndexFileList      []string           `json:"index_file_list"`
	AutoindexEnabled   bool               `json:"autoindex_enabled"`
	AutoindexExactSize bool               `json:"autoindex_exact_size"`
	AutoindexLocaltime bool               `json:"autoindex_localtime"`
	AutoindexFormat    string             `json:"autoindex_format"`
	ErrorPage404       string             `json:"error_page_404"`
	ErrorPage403       string             `json:"error_page_403"`
	AccessLogEnabled   bool               `json:"access_log_enabled"`
	AccessLogPath      string             `json:"access_log_path"`
	ErrorLogPath       string             `json:"error_log_path"`
	IsImported         bool               `json:"is_imported"`
	MarkerStatus       nginx.MarkerStatus `json:"marker_status"`
	ImportWarnings     []string           `json:"import_warnings,omitempty"`
	Proxy              *ProxyBrief        `json:"proxy,omitempty"`
	SSL                *SSLBrief          `json:"ssl,omitempty"`
}

type ProxyBrief struct {
	Enabled bool `json:"enabled"`
}

type SSLBrief struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode,omitempty"`
	NotAfter   string `json:"not_after,omitempty"`
	ForceHTTPS bool   `json:"force_https,omitempty"`
}

type ImportScanItem struct {
	SourceFile      string   `json:"source_file"`
	ServerNames     []string `json:"server_names"`
	Listen          []string `json:"listen"`
	RootPath        string   `json:"root_path"`
	AccessLogPath   string   `json:"access_log_path,omitempty"`
	ErrorLogPath    string   `json:"error_log_path,omitempty"`
	ConfigPathOK    bool     `json:"config_path_ok"`
	RootPathOK      bool     `json:"root_path_ok"`
	AccessLogPathOK bool     `json:"access_log_path_ok"`
	ErrorLogPathOK  bool     `json:"error_log_path_ok"`
	Warnings        []string `json:"warnings,omitempty"`
}

type ImportScanResponse struct {
	Items []ImportScanItem `json:"items"`
}

type ImportSiteRequest struct {
	SourceFile string `json:"source_file"`
}

type ImportSiteResponse struct {
	SiteID      string `json:"site_id"`
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
}
