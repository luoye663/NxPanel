package nginxconf

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

const maxConfSize = 512 * 1024

type agentClient interface {
	ReadFile(ctx context.Context, path string) ([]byte, string, error)
	ApplyTransaction(ctx context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
	WriteBackConfig(ctx context.Context, req *agentclient.ConfigWriteBackRequest) error
}

type confPathGetter interface {
	GetConfPath() string
}

type opRecorder interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

type Service struct {
	agent    agentClient
	confPath confPathGetter
	opRepo   opRecorder
}

func NewService(
	agent agentClient,
	confPath confPathGetter,
	opRepo opRecorder,
) *Service {
	return &Service{
		agent:    agent,
		confPath: confPath,
		opRepo:   opRepo,
	}
}

func (svc *Service) getConfPath() (string, error) {
	confPath := svc.confPath.GetConfPath()
	if confPath == "" {
		return "", app.NewAppError(app.ErrAgentUnavailable, "nginx 配置路径未检测，请先执行 Nginx 检测", nil)
	}
	return confPath, nil
}

func (svc *Service) GetNginxConf(ctx context.Context) (*NginxConfResponse, error) {
	confPath, err := svc.getConfPath()
	if err != nil {
		return nil, err
	}

	content, hash, err := svc.agent.ReadFile(ctx, confPath)
	if err != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取 nginx.conf 失败: "+err.Error(), nil)
	}

	return &NginxConfResponse{
		Content: string(content),
		Hash:    hash,
	}, nil
}

func (svc *Service) SaveNginxConf(ctx context.Context, req *SaveNginxConfRequest, requestID string) (*SaveNginxConfResponse, error) {
	if !req.DangerConfirmed {
		return nil, app.NewAppError(app.ErrValidationFailed, "保存 nginx.conf 需要确认风险（danger_confirmed=true）", nil)
	}

	if len(req.Content) > maxConfSize {
		return nil, app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("配置文件大小超过限制（最大 %d KB）", maxConfSize/1024), nil)
	}

	if strings.Contains(req.Content, "\x00") {
		return nil, app.NewAppError(app.ErrValidationFailed, "内容不允许包含空字节", nil)
	}

	confPath, err := svc.getConfPath()
	if err != nil {
		return nil, err
	}

	if req.ExpectedHash != "" {
		_, currentHash, err := svc.agent.ReadFile(ctx, confPath)
		if err != nil {
			return nil, app.NewAppError(app.ErrAgentUnavailable, "读取 nginx.conf 失败: "+err.Error(), nil)
		}
		if currentHash != req.ExpectedHash {
			return nil, app.NewAppError(app.ErrConfigDrifted, "配置文件已被外部修改，请刷新后重试", nil)
		}
	}

	newHash := nginx.HashContent([]byte(req.Content))

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "nginx.save_conf", TargetType: "system", TargetID: "nginx_conf",
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   "保存 nginx.conf 配置",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{
				Type:          "write",
				Path:          confPath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.Content)),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: true,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "保存 nginx.conf 失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	svc.syncWebUserFromConf(ctx, req.Content)
	slog.Info("nginx.conf 保存成功", "conf_path", confPath, "operation_id", opID)

	return &SaveNginxConfResponse{
		Hash:        newHash,
		OperationID: opID,
	}, nil
}

func (svc *Service) GetNginxParameters(ctx context.Context) (*NginxParametersResponse, error) {
	confPath, err := svc.getConfPath()
	if err != nil {
		return nil, err
	}

	content, _, err := svc.agent.ReadFile(ctx, confPath)
	if err != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取 nginx.conf 失败: "+err.Error(), nil)
	}

	parsed := ParseParameters(string(content))
	params := make([]ParameterValue, len(ParameterDefs))
	for i, def := range ParameterDefs {
		val := parsed[def.Key]
		if val == "" {
			val = def.Default
		}
		params[i] = ParameterValue{
			Key:          def.Key,
			Value:        val,
			DefaultValue: def.Default,
			Description:  def.Description,
			Unit:         def.Unit,
			Group:        def.Group,
			Tooltip:      def.Tooltip,
			Options:      def.Options,
			Clearable:    def.Clearable,
		}
	}

	return &NginxParametersResponse{
		Parameters: params,
		ConfPath:   confPath,
	}, nil
}

func (svc *Service) SaveNginxParameters(ctx context.Context, req *SaveNginxParametersRequest, requestID string) (*SaveNginxParametersResponse, error) {
	confPath, err := svc.getConfPath()
	if err != nil {
		return nil, err
	}

	content, _, err := svc.agent.ReadFile(ctx, confPath)
	if err != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取 nginx.conf 失败: "+err.Error(), nil)
	}

	newContent, err := ApplyParameters(string(content), req.Parameters)
	if err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, "修改参数失败: "+err.Error(), nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "nginx.save_parameters", TargetType: "system", TargetID: "nginx_parameters",
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   "修改 Nginx 常用参数",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{
				Type:          "write",
				Path:          confPath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(newContent)),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: true,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "保存参数失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	svc.syncWebUserFromConf(ctx, newContent)
	slog.Info("nginx 常用参数保存成功", "conf_path", confPath, "operation_id", opID)

	parsed := ParseParameters(newContent)
	params := make([]ParameterValue, len(ParameterDefs))
	for i, def := range ParameterDefs {
		val := parsed[def.Key]
		if val == "" {
			val = def.Default
		}
		params[i] = ParameterValue{
			Key:          def.Key,
			Value:        val,
			DefaultValue: def.Default,
			Description:  def.Description,
			Unit:         def.Unit,
			Group:        def.Group,
			Tooltip:      def.Tooltip,
			Options:      def.Options,
			Clearable:    def.Clearable,
		}
	}

	return &SaveNginxParametersResponse{
		Parameters:  params,
		ConfPath:    confPath,
		OperationID: opID,
	}, nil
}

func (svc *Service) syncWebUserFromConf(ctx context.Context, content string) {
	user, group := nginx.ParseWebUser(content)
	if !isSafeWebUser(user) {
		return
	}
	if strings.TrimSpace(group) == "" {
		group = user
	}
	if !isSafeWebUser(group) {
		return
	}
	if err := svc.agent.WriteBackConfig(ctx, &agentclient.ConfigWriteBackRequest{Fields: []agentclient.ConfigWriteBackField{
		{Key: "nginx.web_user", Value: user},
		{Key: "nginx.web_group", Value: group},
	}}); err != nil {
		slog.Warn("同步 nginx web 用户到配置失败", "user", user, "group", group, "error", err)
	}
}

func isSafeWebUser(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "root"
}
