package nginx

// 标记前缀
const MarkerPrefix = "#NXPANEL-"

// 标记名常量（用于 markerStart / markerEnd）
const (
	MarkerNameSite           = "SITE"
	MarkerNameListen         = "LISTEN"
	MarkerNameServerName     = "SERVER-NAME"
	MarkerNameSSL            = "SSL"
	MarkerNameForceHTTPS     = "FORCE-HTTPS"
	MarkerNameRoot           = "ROOT"
	MarkerNameLog            = "LOG"
	MarkerNameRewrite        = "REWRITE"
	MarkerNameHotlink        = "HOTLINK"
	MarkerNameDocument       = "DOCUMENT"
	MarkerNameMainLocation   = "MAIN-LOCATION"
	MarkerNameExtraLocations = "EXTRA-LOCATIONS"
	MarkerNameInclude        = "INCLUDE"
	MarkerNameAccessLimit    = "ACCESS-LIMIT"
	MarkerNameACMEChallenge  = "ACME-CHALLENGE"
)

// 完整标记字符串常量
const (
	MarkerSiteStart           = "#NXPANEL-SITE-START"
	MarkerSiteEnd             = "#NXPANEL-SITE-END"
	MarkerListenStart         = "#NXPANEL-LISTEN-START"
	MarkerListenEnd           = "#NXPANEL-LISTEN-END"
	MarkerServerNameStart     = "#NXPANEL-SERVER-NAME-START"
	MarkerServerNameEnd       = "#NXPANEL-SERVER-NAME-END"
	MarkerSSLStart            = "#NXPANEL-SSL-START"
	MarkerSSLEnd              = "#NXPANEL-SSL-END"
	MarkerForceHTTPSStart     = "#NXPANEL-FORCE-HTTPS-START"
	MarkerForceHTTPSEnd       = "#NXPANEL-FORCE-HTTPS-END"
	MarkerRootStart           = "#NXPANEL-ROOT-START"
	MarkerRootEnd             = "#NXPANEL-ROOT-END"
	MarkerLogStart            = "#NXPANEL-LOG-START"
	MarkerLogEnd              = "#NXPANEL-LOG-END"
	MarkerRewriteStart        = "#NXPANEL-REWRITE-START"
	MarkerRewriteEnd          = "#NXPANEL-REWRITE-END"
	MarkerHotlinkStart        = "#NXPANEL-HOTLINK-START"
	MarkerHotlinkEnd          = "#NXPANEL-HOTLINK-END"
	MarkerDocumentStart       = "#NXPANEL-DOCUMENT-START"
	MarkerDocumentEnd         = "#NXPANEL-DOCUMENT-END"
	MarkerMainLocationStart   = "#NXPANEL-MAIN-LOCATION-START"
	MarkerMainLocationEnd     = "#NXPANEL-MAIN-LOCATION-END"
	MarkerExtraLocationsStart = "#NXPANEL-EXTRA-LOCATIONS-START"
	MarkerExtraLocationsEnd   = "#NXPANEL-EXTRA-LOCATIONS-END"
	MarkerIncludeStart        = "#NXPANEL-INCLUDE-START"
	MarkerIncludeEnd          = "#NXPANEL-INCLUDE-END"
	MarkerAccessLimitStart    = "#NXPANEL-ACCESS-LIMIT-START"
	MarkerAccessLimitEnd      = "#NXPANEL-ACCESS-LIMIT-END"
	MarkerACMEChallengeStart  = "#NXPANEL-ACME-CHALLENGE-START"
	MarkerACMEChallengeEnd    = "#NXPANEL-ACME-CHALLENGE-END"
)
