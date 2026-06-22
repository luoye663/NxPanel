// nginx 包 — 渲染器数据模型
//
// 定义 Nginx 配置模板渲染所需的参数结构体。
// 渲染器接收这些参数，生成最终的 Nginx 配置文件内容。
package nginx

import "github.com/luoye663/nxpanel/internal/app"

type Binding = app.Binding

// RenderData 是渲染 Nginx 配置所需的全部参数
// 模板中使用 {{ .SiteID }}、{{ .PrimaryDomain }} 等方式访问
type RenderData struct {
	// 站点标识
	SiteID        string // 站点唯一 ID，如 site_xxx
	PrimaryDomain string // 主域名，如 example.com
	ServerNames   string // 所有域名，空格分隔，如 "example.com www.example.com"

	// 监听配置
	HTTPPort      int  // HTTP 监听端口，默认 80
	HTTPSPort     int  // HTTPS 监听端口，默认 443
	DefaultServer bool // 是否为默认站点（listen 指令加 default_server）

	// 域名绑定（多域名多端口）
	// 非空时合并为单个 server 块：所有域名合并到 server_name，所有端口合并为多条 listen 指令
	Bindings []Binding

	// 静态站点配置
	RootPath   string
	IndexFiles string

	// 日志配置
	AccessLogEnabled bool   // 是否开启 access_log
	AccessLogPath    string // access_log 路径
	ErrorLogPath     string // error_log 路径

	// 自定义 Location 文件路径
	RewritePath string // include 的 rewrite 文件路径

	// 访问限制文件路径
	AccessLimitPath string // include 的 access-limit 文件路径
	HotlinkPath     string // include 的 hotlink 文件路径

	// 文档增强配置
	Document DocumentData

	// 反向代理配置（多代理支持）
	Proxies        []*ProxyData // 代理列表，为空表示未开启反代
	Proxy          *ProxyData   // 兼容旧代码，指向第一个代理或 nil
	ExtraLocations string       // 额外的 location 块（非 / 路径的代理）

	// SSL 配置
	SSL *SSLData // 为 nil 表示未开启 SSL

	// 以下字段由 renderer 内部函数生成，用于模板渲染
	MainLocation       string
	ListenBlock        string
	SSLBlock           string
	ForceHTTPSBlock    string
	LogBlock           string
	DocumentBlock      string
	ACMEChallengeBlock string
}

type DocumentData struct {
	AutoindexEnabled   bool
	AutoindexExactSize bool
	AutoindexLocaltime bool
	AutoindexFormat    string
	ErrorPage404       string
	ErrorPage403       string
}

// ProxyData 反向代理渲染参数
type ProxyData struct {
	ID               string
	Name             string
	Enabled          bool
	LocationPath     string // 如 /, /api
	UpstreamURL      string // 如 http://127.0.0.1:3000
	HostHeader       string // 如 $host
	WebSocketEnabled bool
	ConnectTimeout   int // 秒
	SendTimeout      int // 秒
	ReadTimeout      int // 秒
	CacheEnabled     bool
	CacheType        string // "nginx" or "file"
	CacheTime        int    // 分钟
	CachePath        string // 文件缓存路径
}

// SSLData SSL 渲染参数
type SSLData struct {
	Enabled    bool
	Mode       string // manual_pem / existing_files
	CertPath   string // 证书文件路径
	KeyPath    string // 私钥文件路径
	ForceHTTPS bool   // 是否强制 HTTPS
}
