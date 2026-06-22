package settings

type DefaultPagesSettings struct {
	NewSitePage      string `json:"new_site_page"`
	Page404          string `json:"page_404"`
	SiteNotFoundPage string `json:"site_not_found_page"`
	SiteDisabledPage string `json:"site_disabled_page"`
}

type DefaultSiteSettings struct {
	SiteID        string `json:"site_id"`
	PrimaryDomain string `json:"primary_domain"`
}

type HTTPSHijackSettings struct {
	Enabled      bool   `json:"enabled"`
	ReturnStatus int    `json:"return_status_code"`
	CertMode     string `json:"cert_mode"`
	CustomCertID string `json:"custom_cert_id,omitempty"`
	CertPath     string `json:"cert_path,omitempty"`
	KeyPath      string `json:"key_path,omitempty"`
}

type UpdateDefaultPagesRequest struct {
	NewSitePage      string `json:"new_site_page"`
	Page404          string `json:"page_404"`
	SiteNotFoundPage string `json:"site_not_found_page"`
	SiteDisabledPage string `json:"site_disabled_page"`
}

type UpdateDefaultSiteRequest struct {
	SiteID string `json:"site_id"`
}

type UpdateHTTPSHijackRequest struct {
	Enabled      bool   `json:"enabled"`
	ReturnStatus int    `json:"return_status_code"`
	CertMode     string `json:"cert_mode"`
	CustomCertID string `json:"custom_cert_id"`
}

type LogRotateSettings struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`
	MaxCount int    `json:"max_count"`
	MaxAge   string `json:"max_age"`
	MinSize  string `json:"min_size"`
}

type UpdateLogRotateRequest struct {
	Enabled  *bool   `json:"enabled"`
	Interval *string `json:"interval"`
	MaxCount *int    `json:"max_count"`
	MaxAge   *string `json:"max_age"`
	MinSize  *string `json:"min_size"`
}

type SecuritySettings struct {
	LoginPath              string   `json:"login_path"`
	PublicHealth           bool     `json:"public_health"`
	RateLimitMaxFailures   int      `json:"rate_limit_max_failures"`
	RateLimitWindow        string   `json:"rate_limit_window"`
	MaxSessions            int      `json:"max_sessions"`
	BindSessionIP          bool     `json:"bind_session_ip"`
	BindSessionUA          bool     `json:"bind_session_ua"`
	TrustedProxies         []string `json:"trusted_proxies"`
	CaptchaProvider        string   `json:"captcha_provider"`
	CaptchaSiteKey         string   `json:"captcha_site_key"`
	CaptchaSecretKeyMasked string   `json:"captcha_secret_key_masked"`
	CaptchaTriggerAfter    int      `json:"captcha_trigger_after_failures"`
	TLSEnabled             bool     `json:"tls_enabled"`
	TLSCert                string   `json:"tls_cert"`
	TLSKey                 string   `json:"tls_key"`
	TLSCertValidity        string   `json:"tls_cert_validity"`
}

type UpdateSecuritySettingsRequest struct {
	LoginPath            *string   `json:"login_path"`
	PublicHealth         *bool     `json:"public_health"`
	RateLimitMaxFailures *int      `json:"rate_limit_max_failures"`
	RateLimitWindow      *string   `json:"rate_limit_window"`
	MaxSessions          *int      `json:"max_sessions"`
	BindSessionIP        *bool     `json:"bind_session_ip"`
	BindSessionUA        *bool     `json:"bind_session_ua"`
	TrustedProxies       *[]string `json:"trusted_proxies"`
	CaptchaProvider      *string   `json:"captcha_provider"`
	CaptchaSiteKey       *string   `json:"captcha_site_key"`
	CaptchaSecretKey     *string   `json:"captcha_secret_key"`
	CaptchaTriggerAfter  *int      `json:"captcha_trigger_after_failures"`
	TLSEnabled           *bool     `json:"tls_enabled"`
	TLSCert              *string   `json:"tls_cert"`
	TLSKey               *string   `json:"tls_key"`
	TLSCertValidity      *string   `json:"tls_cert_validity"`
}
