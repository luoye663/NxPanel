package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/parser"
)

const maxSize = 512 * 1024

type siteStore interface {
	GetByID(id string) (*repo.Site, error)
	Update(s *repo.Site) error
}

type proxyGetter interface {
	GetBySiteID(siteID string) (*repo.SiteProxy, error)
}

type sslGetter interface {
	GetBySiteID(siteID string) (*repo.SiteSSL, error)
}

type opRecorder interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

type agentClient interface {
	ApplyTransaction(ctx context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
	ReadFile(ctx context.Context, path string) ([]byte, string, error)
}

type Service struct {
	siteRepo  siteStore
	proxyRepo proxyGetter
	sslRepo   sslGetter
	opRepo    opRecorder
	agent     agentClient
}

func NewService(
	siteRepo siteStore,
	proxyRepo proxyGetter,
	sslRepo sslGetter,
	opRepo opRecorder,
	agent agentClient,
) *Service {
	return &Service{
		siteRepo:  siteRepo,
		proxyRepo: proxyRepo,
		sslRepo:   sslRepo,
		opRepo:    opRepo,
		agent:     agent,
	}
}

type GetResponse struct {
	Content         string             `json:"content"`
	ContentHash     string             `json:"hash"`
	IsImported      bool               `json:"is_imported"`
	MarkerStatus    nginx.MarkerStatus `json:"marker_status"`
	RequiredMarkers []string           `json:"required_markers"`
}

type SaveRequest struct {
	Content             string `json:"content"`
	ExpectedContentHash string `json:"expected_hash"`
	DangerConfirmed     bool   `json:"danger_confirmed"`
}

type SaveResponse struct {
	ContentHash  string             `json:"hash"`
	MarkerStatus nginx.MarkerStatus `json:"marker_status"`
	SyncWarnings []string           `json:"sync_warnings"`
	OperationID  string             `json:"operation_id"`
}

func (svc *Service) Get(siteID string) (*GetResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	content := ""
	var contentHash string
	if fileContent, hash, err := svc.agent.ReadFile(context.Background(), site.ConfigPath); err == nil {
		content = string(fileContent)
		contentHash = hash
	} else if app.IsPathDeniedError(err) {
		return nil, app.NewPathDeniedError("读取站点配置", "配置文件", site.ConfigPath)
	} else {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+err.Error(), nil)
	}

	required := nginx.RequiredSiteMarkers()
	markerStatus := nginx.ValidateRequiredMarkers([]byte(content), required)

	return &GetResponse{
		Content:         content,
		ContentHash:     contentHash,
		IsImported:      site.ConfigPath == site.EnabledPath,
		MarkerStatus:    markerStatus,
		RequiredMarkers: required,
	}, nil
}

func (svc *Service) Save(ctx context.Context, siteID string, req *SaveRequest, requestID string) (*SaveResponse, error) {
	if !req.DangerConfirmed {
		return nil, app.NewAppError(app.ErrValidationFailed, "保存完整配置需要确认风险（danger_confirmed=true）", nil)
	}

	if len(req.Content) > maxSize {
		return nil, app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("配置文件大小超过限制（最大 %d KB）", maxSize/1024), nil)
	}

	if strings.Contains(req.Content, "\x00") {
		return nil, app.NewAppError(app.ErrValidationFailed, "内容不允许包含空字节", nil)
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	isImported := site.ConfigPath == site.EnabledPath

	if !isImported {
		if err := validateSiteMarkers(req.Content, siteID); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}

	_, currentHash, hashErr := svc.agent.ReadFile(ctx, site.ConfigPath)
	if hashErr != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+hashErr.Error(), nil)
	}
	if req.ExpectedContentHash != "" && currentHash != req.ExpectedContentHash {
		return nil, app.NewAppError(app.ErrConfigDrifted, "配置文件已被外部修改，请刷新后重试", nil)
	}

	newHash := nginx.HashContent([]byte(req.Content))

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.save_config", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("保存完整配置 站点 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{
				Type:          "write",
				Path:          site.ConfigPath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.Content)),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "保存配置失败: "+agentErr.Error(), nil)
	}

	syncWarnings := syncSiteFromConfig(site, []byte(req.Content))

	if isImported {
		// Imported sites have no marker blocks; extract log paths from raw content.
		syncWarnings = nil
		accessLog, errorLog := parser.ExtractServerLogPaths(req.Content)
		site.AccessLogPath = accessLog
		site.ErrorLogPath = errorLog
	}

	if len(syncWarnings) > 0 {
		warningStr := strings.Join(syncWarnings, "; ")
		site.LastSyncWarning = warningStr
	} else {
		site.LastSyncWarning = ""
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	required := nginx.RequiredSiteMarkers()
	markerStatus := nginx.ValidateRequiredMarkers([]byte(req.Content), required)

	slog.Info("完整配置保存成功", "site_id", siteID, "operation_id", opID)

	return &SaveResponse{
		ContentHash:  newHash,
		MarkerStatus: markerStatus,
		SyncWarnings: syncWarnings,
		OperationID:  opID,
	}, nil
}

func validateSiteMarkers(content, expectedSiteID string) error {
	if !strings.Contains(content, nginx.MarkerSiteStart) {
		return fmt.Errorf("配置文件缺少 %s 标识块", nginx.MarkerSiteStart)
	}
	if !strings.Contains(content, nginx.MarkerSiteEnd) {
		return fmt.Errorf("配置文件缺少 %s 标识块", nginx.MarkerSiteEnd)
	}

	expected := "site_id=" + expectedSiteID
	found := false
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, nginx.MarkerSiteStart) {
			if strings.Contains(line, expected) {
				found = true
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("配置文件 site_id 不匹配（期望 site_id=%s）", expectedSiteID)
	}

	return nil
}

func syncSiteFromConfig(site *repo.Site, content []byte) []string {
	var warnings []string

	serverNameBody, err := nginx.ExtractMarkerBlock(content, "SERVER-NAME")
	if err == nil {
		line := strings.TrimSpace(string(serverNameBody))
		line = strings.TrimPrefix(line, "server_name ")
		line = strings.TrimSuffix(line, ";")
		line = strings.TrimSpace(line)
		if line != "" {
			domains := strings.Fields(line)
			domainsJSON, _ := json.Marshal(domains)
			site.DomainsJSON = string(domainsJSON)
			if len(domains) > 0 {
				site.PrimaryDomain = domains[0]
			}
		}
	} else {
		warnings = append(warnings, "无法同步 SERVER-NAME 标识块")
	}

	listenBody, err := nginx.ExtractMarkerBlock(content, "LISTEN")
	if err == nil {
		listenStr := string(listenBody)
		if strings.Contains(listenStr, "ssl") {
			site.HTTPSPort = extractPort(listenStr, "ssl")
		}
		port := extractHTTPPort(listenStr)
		if port > 0 {
			site.HTTPPort = port
		}
	} else {
		warnings = append(warnings, "无法同步 LISTEN 标识块")
	}

	rootBody, err := nginx.ExtractMarkerBlock(content, "ROOT")
	if err == nil {
		lines := strings.Split(string(rootBody), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "root ") {
				site.RootPath = strings.TrimSuffix(strings.TrimPrefix(l, "root "), ";")
			}
			if strings.HasPrefix(l, "index ") {
				site.IndexFiles = strings.TrimSuffix(strings.TrimPrefix(l, "index "), ";")
			}
		}
	} else {
		warnings = append(warnings, "无法同步 ROOT 标识块")
	}

	logBody, err := nginx.ExtractMarkerBlock(content, "LOG")
	if err == nil {
		logStr := string(logBody)
		site.AccessLogEnabled = !strings.Contains(logStr, "access_log off;")
		for _, l := range strings.Split(logStr, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "access_log ") && !strings.Contains(l, "off") {
				site.AccessLogPath = strings.TrimSuffix(strings.TrimPrefix(l, "access_log "), ";")
			}
			if strings.HasPrefix(l, "error_log ") {
				site.ErrorLogPath = strings.TrimSuffix(strings.TrimPrefix(l, "error_log "), ";")
			}
		}
	} else {
		warnings = append(warnings, "无法同步 LOG 标识块")
	}

	return warnings
}

func extractHTTPPort(listenStr string) int {
	for _, l := range strings.Split(listenStr, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "listen ") && !strings.Contains(l, "ssl") {
			portStr := strings.TrimSuffix(strings.TrimPrefix(l, "listen "), ";")
			portStr = strings.TrimSpace(portStr)
			var port int
			fmt.Sscanf(portStr, "%d", &port)
			return port
		}
	}
	return 0
}

func extractPort(listenStr, suffix string) int {
	for _, l := range strings.Split(listenStr, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "listen ") && strings.Contains(l, suffix) {
			portStr := strings.TrimSuffix(strings.TrimPrefix(l, "listen "), ";")
			portStr = strings.TrimSpace(portStr)
			parts := strings.Fields(portStr)
			if len(parts) > 0 {
				var port int
				fmt.Sscanf(parts[0], "%d", &port)
				return port
			}
		}
	}
	return 443
}
