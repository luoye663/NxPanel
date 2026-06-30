// repo 包 — 数据访问层
//
// 本文件定义所有 repository 共用的模型结构体。
// 这些结构体直接映射数据库表行，供 repository 方法使用。
//
// 注意：SQLite 使用 TEXT 存储时间戳，Scan 无法直接存入 time.Time。
// 因此模型中的时间字段使用 string 类型，由调用方按需解析。
package repo

import "github.com/luoye663/nxpanel/internal/app"

type Binding = app.Binding

// Admin 对应 admin_account 表的一行
type Admin struct {
	ID            int
	Username      string
	PasswordHash  string
	PasswordAlgo  string
	TOTPSecret    string
	TOTPEnabled   bool
	RecoveryCodes string
	LastTOTPCode  string
	LastTOTPTime  string
	CreatedAt     string
	UpdatedAt     string
}

// Session 对应 sessions 表的一行
type Session struct {
	ID            string
	CSRFTokenHash string
	UserAgent     string
	IP            string
	ExpiresAt     string
	CreatedAt     string
	LastSeenAt    string
}

// Site 对应 sites 表的一行
type Site struct {
	ID                 string
	PrimaryDomain      string
	DomainsJSON        string
	BindingsJSON       string
	Status             string
	HTTPPort           int
	HTTPSPort          int
	RootPath           string
	IndexFiles         string
	AccessLogEnabled   bool
	AccessLogPath      string
	ErrorLogPath       string
	ConfigPath         string
	EnabledPath        string
	RewritePath        string
	AccessLimitPath    string
	HotlinkPath        string
	AutoindexEnabled   bool
	AutoindexExactSize bool
	AutoindexLocaltime bool
	AutoindexFormat    string
	ErrorPage404       string
	ErrorPage403       string
	MarkerVersion      int
	LastSyncWarning    string
	CreatedAt          string
	UpdatedAt          string
}

// SiteHotlinkRule 对应 site_hotlink_rules 表的一行（防盗链规则）
type SiteHotlinkRule struct {
	ID                string
	SiteID            string
	Name              string
	Enabled           bool
	Extensions        string
	Referers          string
	AllowEmptyReferer bool
	BlockStatus       int
	SortOrder         int
	CreatedAt         string
	UpdatedAt         string
}

// SiteProxy 对应 site_proxy 表的一行
type SiteProxy struct {
	ID               string
	SiteID           string
	Name             string
	Enabled          bool
	LocationPath     string
	UpstreamURL      string
	HostHeader       string
	WebSocketEnabled bool
	ConnectTimeout   int
	SendTimeout      int
	ReadTimeout      int
	CacheEnabled     bool
	CacheType        string // "nginx" or "file"
	CacheTime        int    // 分钟
	AuthEnabled      bool
	AuthHtpasswdPath string
	CreatedAt        string
	UpdatedAt        string
}

// AuthAccount 对应 auth_accounts 表的一行。
type AuthAccount struct {
	ID           string
	Scope        string
	SiteID       string
	Username     string
	PasswordHash string
	Enabled      bool
	CreatedAt    string
	UpdatedAt    string
}

// SiteSSL 对应 site_ssl 表的一行
type SiteSSL struct {
	SiteID       string
	Enabled      bool
	Mode         string
	CertPath     string
	KeyPath      string
	CertSHA256   string
	KeySHA256    string
	Issuer       string
	Subject      string
	NotBefore    *string
	NotAfter     *string
	DNSNamesJSON string
	ForceHTTPS   bool
	HSTSEnabled  bool
	CertStoreID  *string
	CreatedAt    string
	UpdatedAt    string
}

// Certificate 对应 certificates 表的一行（证书夹）
type Certificate struct {
	ID          string
	Name        string
	DomainsJSON string
	Issuer      string
	Subject     string
	NotBefore   *string
	NotAfter    *string
	CertSHA256  string
	KeySHA256   string
	CertPath    string
	KeyPath     string
	CreatedAt   string
	UpdatedAt   string
}

// SiteRewrite 对应 site_rewrite 表的一行
type SiteRewrite struct {
	SiteID      string
	ContentHash string
	SizeBytes   int
	UpdatedAt   string
}

// RewriteTemplateParam Location 模板的单个参数定义
type RewriteTemplateParam struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Default  any      `json:"default"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

// RewriteTemplate 对应 rewrite_templates 表的一行
type RewriteTemplate struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Category    string                 `json:"category"`
	Description string                 `json:"description"`
	Params      []RewriteTemplateParam `json:"params"`
	Template    string                 `json:"template"`
	Enabled     bool                   `json:"enabled"`
	SortOrder   int                    `json:"sort_order"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

// Operation 对应 operations 表的一行
type Operation struct {
	ID           string
	Action       string
	TargetType   string
	TargetID     string
	Status       string
	RequestID    string
	Actor        string
	IP           string
	UserAgent    string
	Message      string
	ErrorCode    string
	ErrorMessage string
	Stderr       string
	CreatedAt    string
	FinishedAt   *string
}

// Backup 对应 backups 表的一行
type Backup struct {
	ID             string
	OperationID    string
	FilePath       string
	BackupPath     string
	OriginalSHA256 string
	BackupSHA256   string
	FileExisted    bool
	CreatedAt      string
}

// SiteBackup 对应 site_backups 表的一行（用户手动创建的站点备份）
type SiteBackup struct {
	ID         string
	SiteID     string
	BackupType string
	Name       string
	BackupPath string
	SizeBytes  int64
	Status     string
	Message    string
	CreatedAt  string
	UpdatedAt  string
	FinishedAt *string
}

// SiteBackupSchedule 对应 site_backup_schedules 表的一行
type SiteBackupSchedule struct {
	SiteID         string
	Enabled        bool
	BackupType     string
	BackupDir      string
	RetentionCount int
	ScheduleType   string
	ScheduleTime   string
	Weekday        int
	MonthDay       int
	LastRunAt      *string
	CreatedAt      string
	UpdatedAt      string
}

// Setting 对应 settings 表的一行
type Setting struct {
	Key       string
	Value     string
	UpdatedAt string
}

// SiteAuthRule 对应 site_auth_rules 表的一行（加密访问规则）
type SiteAuthRule struct {
	ID           string
	SiteID       string
	Name         string
	Path         string
	Username     string
	PasswordHash string
	HtpasswdPath string
	Enabled      bool
	SortOrder    int
	CreatedAt    string
	UpdatedAt    string
}

// SiteDenyRule 对应 site_deny_rules 表的一行（禁止访问规则）
type SiteDenyRule struct {
	ID               string
	SiteID           string
	Name             string
	DenyType         string
	Pattern          string
	ExtensionPattern string
	PathPattern      string
	Enabled          bool
	SortOrder        int
	CreatedAt        string
	UpdatedAt        string
}

// SiteIPWhitelistRule 对应 site_ip_whitelist_rules 表的一行（站点 IP 白名单规则）
type SiteIPWhitelistRule struct {
	ID        string
	SiteID    string
	Name      string
	RuleType  string
	IPsJSON   string
	Enabled   bool
	SortOrder int
	CreatedAt string
	UpdatedAt string
}

type LoginAudit struct {
	ID              int
	Username        string
	IP              string
	UserAgent       string
	Success         bool
	FailureReason   string
	CaptchaVerified bool
	TOTPUsed        bool
	CreatedAt       string
}
