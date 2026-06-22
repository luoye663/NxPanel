// rewrite 包 — 自定义 Location 业务服务
//
// 提供站点自定义 Location 文件的读取和保存功能，以及 Location 模板的动态管理。
// 自定义 Location 文件允许用户写任意 server-level Nginx 片段。
//
// 规则：
//   - 允许任意 server-level 片段
//   - 最大 256KB
//   - 保存前必须 danger_confirmed=true
//   - 必须 nginx -t + reload
//   - 失败回滚
//   - 禁止空字节
package rewrite

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

// maxSize 自定义 Location 文件最大大小 256KB
const maxSize = 256 * 1024

// maxTemplateSize 单个模板体最大大小 64KB
const maxTemplateSize = 64 * 1024

// templateKeyPattern 模板参数 key 允许字符：字母/下划线开头
var templateKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// siteGetter 获取站点（由 SiteRepo 实现）
type siteGetter interface {
	GetByID(id string) (*repo.Site, error)
}

// rewriteMeta 自定义 Location 元信息读写（由 RewriteRepo 实现）
type rewriteMeta interface {
	GetBySiteID(siteID string) (*repo.SiteRewrite, error)
	Upsert(sr *repo.SiteRewrite) error
}

// templateStore Location 模板读写（由 RewriteTemplateRepo 实现）
type templateStore interface {
	List() ([]repo.RewriteTemplate, error)
	ListEnabled() ([]repo.RewriteTemplate, error)
	GetByID(id string) (*repo.RewriteTemplate, error)
	Create(t *repo.RewriteTemplate) error
	Update(t *repo.RewriteTemplate) error
	Delete(id string) error
}

// opRecorder 操作记录（由 OperationRepo 实现）
type opRecorder interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

// agentTx agent 文件事务能力（由 agentclient.Client 实现）
type agentTx interface {
	ApplyTransaction(ctx context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
	ReadFile(ctx context.Context, path string) ([]byte, string, error)
}

// Service 自定义 Location 业务服务
type Service struct {
	siteRepo     siteGetter
	rewriteRepo  rewriteMeta
	opRepo       opRecorder
	agent        agentTx
	templateRepo templateStore
}

// NewService 创建自定义 Location 服务
func NewService(
	siteRepo siteGetter,
	rewriteRepo rewriteMeta,
	opRepo opRecorder,
	agent agentTx,
	templateRepo templateStore,
) *Service {
	return &Service{
		siteRepo:     siteRepo,
		rewriteRepo:  rewriteRepo,
		opRepo:       opRepo,
		agent:        agent,
		templateRepo: templateRepo,
	}
}

// GetResponse 获取自定义 Location 内容响应
// 对应文档 7.6.1 节
type GetResponse struct {
	Content     string `json:"content"`
	ContentHash string `json:"content_hash"`
	Path        string `json:"path"`
	SizeBytes   int    `json:"size_bytes"`
}

// UpdateRequest 保存自定义 Location 内容请求
// 对应文档 7.6.2 节
type UpdateRequest struct {
	Content             string `json:"content"`
	ExpectedContentHash string `json:"expected_content_hash"`
	DangerConfirmed     bool   `json:"danger_confirmed"`
}

// UpdateResponse 保存自定义 Location 内容响应
type UpdateResponse struct {
	ContentHash string `json:"content_hash"`
	OperationID string `json:"operation_id"`
}

type TemplateListResponse struct {
	Templates []repo.RewriteTemplate `json:"templates"`
}

type TemplatePreviewRequest struct {
	TemplateID string         `json:"template_id"`
	Params     map[string]any `json:"params"`
}

type TemplatePreviewResponse struct {
	Content string `json:"content"`
}

type ApplyTemplateRequest struct {
	TemplateID          string         `json:"template_id"`
	Params              map[string]any `json:"params"`
	ExpectedContentHash string         `json:"expected_content_hash"`
	Mode                string         `json:"mode"`
	DangerConfirmed     bool           `json:"danger_confirmed"`
}

// TemplateInput 创建/更新模板时的用户输入
type TemplateInput struct {
	Name        string                      `json:"name"`
	Category    string                      `json:"category"`
	Description string                      `json:"description"`
	Params      []repo.RewriteTemplateParam `json:"params"`
	Template    string                      `json:"template"`
	Enabled     *bool                       `json:"enabled"`
	SortOrder   *int                        `json:"sort_order"`
}

// ============================================================
// 模板管理（CRUD）
// ============================================================

// ListTemplates 返回全部模板（含禁用项与模板体，供管理页使用；应用弹窗前端按 enabled 过滤）
func (svc *Service) ListTemplates() (*TemplateListResponse, error) {
	if svc.templateRepo == nil {
		return &TemplateListResponse{Templates: []repo.RewriteTemplate{}}, nil
	}
	items, err := svc.templateRepo.List()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if items == nil {
		items = []repo.RewriteTemplate{}
	}
	return &TemplateListResponse{Templates: items}, nil
}

// CreateTemplate 创建模板
func (svc *Service) CreateTemplate(input *TemplateInput) (*repo.RewriteTemplate, error) {
	if svc.templateRepo == nil {
		return nil, app.NewAppError(app.ErrInternalError, "模板存储不可用", nil)
	}
	if err := validateTemplateInput(input); err != nil {
		return nil, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	sortOrder := 0
	if input.SortOrder != nil {
		sortOrder = *input.SortOrder
	}
	tpl := &repo.RewriteTemplate{
		ID:          app.NewID("rwtpl"),
		Name:        input.Name,
		Category:    input.Category,
		Description: input.Description,
		Params:      input.Params,
		Template:    input.Template,
		Enabled:     enabled,
		SortOrder:   sortOrder,
	}
	if err := svc.templateRepo.Create(tpl); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return tpl, nil
}

// UpdateTemplate 更新模板
func (svc *Service) UpdateTemplate(id string, input *TemplateInput) (*repo.RewriteTemplate, error) {
	if svc.templateRepo == nil {
		return nil, app.NewAppError(app.ErrInternalError, "模板存储不可用", nil)
	}
	if id == "" {
		return nil, app.NewAppError(app.ErrBadRequest, "template_id 不能为空", nil)
	}
	if err := validateTemplateInput(input); err != nil {
		return nil, err
	}
	existing, err := svc.templateRepo.GetByID(id)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if existing == nil {
		return nil, app.NewAppError(app.ErrNotFound, "Location 模板不存在", nil)
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	sortOrder := existing.SortOrder
	if input.SortOrder != nil {
		sortOrder = *input.SortOrder
	}
	updated := &repo.RewriteTemplate{
		ID:          id,
		Name:        input.Name,
		Category:    input.Category,
		Description: input.Description,
		Params:      input.Params,
		Template:    input.Template,
		Enabled:     enabled,
		SortOrder:   sortOrder,
	}
	if err := svc.templateRepo.Update(updated); err != nil {
		if err.Error() != "" && strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return nil, app.NewAppError(app.ErrNotFound, "Location 模板不存在", nil)
		}
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return updated, nil
}

// DeleteTemplate 删除模板
func (svc *Service) DeleteTemplate(id string) error {
	if svc.templateRepo == nil {
		return app.NewAppError(app.ErrInternalError, "模板存储不可用", nil)
	}
	if id == "" {
		return app.NewAppError(app.ErrBadRequest, "template_id 不能为空", nil)
	}
	if err := svc.templateRepo.Delete(id); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return app.NewAppError(app.ErrNotFound, "Location 模板不存在", nil)
		}
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return nil
}

// validateTemplateInput 校验模板输入（名称/分类/描述/参数/模板体）
func validateTemplateInput(input *TemplateInput) error {
	if input == nil {
		return app.NewAppError(app.ErrBadRequest, "请求体不能为空", nil)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return app.NewAppError(app.ErrValidationFailed, "模板名称不能为空", nil)
	}
	if len(name) > 64 {
		return app.NewAppError(app.ErrValidationFailed, "模板名称不能超过 64 个字符", nil)
	}
	if strings.ContainsAny(name, "\x00") {
		return app.NewAppError(app.ErrValidationFailed, "模板名称包含非法字符", nil)
	}
	if len(input.Category) > 32 {
		return app.NewAppError(app.ErrValidationFailed, "分类不能超过 32 个字符", nil)
	}
	if strings.ContainsAny(input.Category, "\x00") {
		return app.NewAppError(app.ErrValidationFailed, "分类包含非法字符", nil)
	}
	if len(input.Description) > 256 {
		return app.NewAppError(app.ErrValidationFailed, "描述不能超过 256 个字符", nil)
	}
	if strings.ContainsAny(input.Description, "\x00") {
		return app.NewAppError(app.ErrValidationFailed, "描述包含非法字符", nil)
	}

	body := strings.TrimSpace(input.Template)
	if body == "" {
		return app.NewAppError(app.ErrValidationFailed, "模板内容不能为空", nil)
	}
	if strings.ContainsAny(input.Template, "\x00") {
		return app.NewAppError(app.ErrValidationFailed, "模板内容包含空字节", nil)
	}
	if len(input.Template) > maxTemplateSize {
		return app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("模板内容超过限制（最大 %d KB）", maxTemplateSize/1024), nil)
	}

	seenKeys := make(map[string]bool, len(input.Params))
	for _, p := range input.Params {
		key := strings.TrimSpace(p.Key)
		if key == "" {
			return app.NewAppError(app.ErrValidationFailed, "参数 key 不能为空", nil)
		}
		if len(key) > 32 || !templateKeyPattern.MatchString(key) {
			return app.NewAppError(app.ErrValidationFailed,
				fmt.Sprintf("参数 key %s 非法（仅允许字母/数字/下划线，字母或下划线开头）", key), nil)
		}
		if seenKeys[key] {
			return app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 key %s 重复", key), nil)
		}
		seenKeys[key] = true

		label := strings.TrimSpace(p.Label)
		if label == "" {
			return app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 的 label 不能为空", key), nil)
		}
		if len(label) > 64 {
			return app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 的 label 过长", key), nil)
		}
		switch p.Type {
		case "string", "number", "boolean", "select":
		default:
			return app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 的 type 非法", key), nil)
		}
		if p.Type == "select" && len(p.Options) == 0 {
			return app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 为 select 类型但未提供 options", key), nil)
		}
	}
	return nil
}

// PreviewTemplate 渲染预览（不落盘）
func (svc *Service) PreviewTemplate(req *TemplatePreviewRequest) (*TemplatePreviewResponse, error) {
	tpl, err := svc.loadTemplate(req.TemplateID)
	if err != nil {
		return nil, err
	}
	content, err := renderTemplateContent(*tpl, req.Params)
	if err != nil {
		return nil, err
	}
	return &TemplatePreviewResponse{Content: content}, nil
}

// ApplyTemplate 渲染模板并应用到站点自定义 Location 文件
func (svc *Service) ApplyTemplate(ctx context.Context, siteID string, req *ApplyTemplateRequest, requestID string) (*UpdateResponse, error) {
	if !req.DangerConfirmed {
		return nil, app.NewAppError(app.ErrValidationFailed, "应用模板需要确认风险（danger_confirmed=true）", nil)
	}
	tpl, err := svc.loadTemplate(req.TemplateID)
	if err != nil {
		return nil, err
	}
	content, err := renderTemplateContent(*tpl, req.Params)
	if err != nil {
		return nil, err
	}
	mode := req.Mode
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "append" {
		return nil, app.NewAppError(app.ErrValidationFailed, "mode 必须是 replace 或 append", nil)
	}
	finalContent := content
	if mode == "append" {
		current, err := svc.Get(ctx, siteID)
		if err != nil {
			return nil, err
		}
		finalContent = strings.TrimRight(current.Content, "\n") + "\n\n# NXPANEL-REWRITE-TEMPLATE " + req.TemplateID + " START\n" + content + "# NXPANEL-REWRITE-TEMPLATE " + req.TemplateID + " END\n"
	}
	return svc.Update(ctx, siteID, &UpdateRequest{Content: finalContent, ExpectedContentHash: req.ExpectedContentHash, DangerConfirmed: true}, requestID)
}

// loadTemplate 从存储加载模板（含禁用项，保证已应用过的模板仍可预览）
func (svc *Service) loadTemplate(id string) (*repo.RewriteTemplate, error) {
	if svc.templateRepo == nil {
		return nil, app.NewAppError(app.ErrNotFound, "Location 模板不存在", nil)
	}
	tpl, err := svc.templateRepo.GetByID(id)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if tpl == nil {
		return nil, app.NewAppError(app.ErrNotFound, "Location 模板不存在", nil)
	}
	return tpl, nil
}

// Get 获取站点自定义 Location 文件内容
func (svc *Service) Get(ctx context.Context, siteID string) (*GetResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	// 通过 agent 读取文件内容
	var content string
	var fileHash string
	fileData, hash, err := svc.agent.ReadFile(ctx, site.RewritePath)
	if err != nil {
		slog.Warn("读取自定义 Location 文件失败，返回空内容", "site_id", siteID, "path", site.RewritePath, "error", err)
	} else {
		content = string(fileData)
		fileHash = hash
	}

	// 获取 DB 中的元信息
	rw, _ := svc.rewriteRepo.GetBySiteID(siteID)

	contentHash := fileHash
	sizeBytes := len(fileData)
	if rw != nil {
		if rw.ContentHash != "" {
			contentHash = rw.ContentHash
		}
		if rw.SizeBytes > 0 {
			sizeBytes = rw.SizeBytes
		}
	}

	return &GetResponse{
		Content:     content,
		ContentHash: contentHash,
		Path:        site.RewritePath,
		SizeBytes:   sizeBytes,
	}, nil
}

// Update 保存自定义 Location 内容
func (svc *Service) Update(ctx context.Context, siteID string, req *UpdateRequest, requestID string) (*UpdateResponse, error) {
	// 校验 danger_confirmed
	if !req.DangerConfirmed {
		return nil, app.NewAppError(app.ErrValidationFailed, "保存自定义 Location 需要确认风险（danger_confirmed=true）", nil)
	}

	// 基本安全检查
	if strings.Contains(req.Content, "\x00") {
		return nil, app.NewAppError(app.ErrValidationFailed, "内容不允许包含空字节", nil)
	}

	// 大小限制
	if len(req.Content) > maxSize {
		return nil, app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("自定义 Location 文件大小超过限制（最大 %d KB）", maxSize/1024), nil)
	}

	// 检查站点
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	// 乐观并发控制（expected_content_hash）
	if req.ExpectedContentHash != "" {
		rw, _ := svc.rewriteRepo.GetBySiteID(siteID)
		if rw != nil && rw.ContentHash != req.ExpectedContentHash {
			return nil, app.NewAppError(app.ErrConfigDrifted,
				"自定义 Location 文件已被修改，请刷新后重试", nil)
		}
	}

	// 计算新 hash
	newHash := nginx.HashContent([]byte(req.Content))

	// 操作记录
	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.update_rewrite", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("保存自定义 Location 站点 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// 通过 agent 写入文件
	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{
				Type:          "write",
				Path:          site.RewritePath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.Content)),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "保存自定义 Location 失败: "+agentErr.Error(), nil)
	}

	// 更新 DB 元信息
	_ = svc.rewriteRepo.Upsert(&repo.SiteRewrite{
		SiteID:      siteID,
		ContentHash: newHash,
		SizeBytes:   len(req.Content),
	})
	_ = svc.opRepo.UpdateStatus(opID, "success")

	slog.Info("自定义 Location 保存成功", "site_id", siteID, "size", len(req.Content), "operation_id", opID)

	return &UpdateResponse{
		ContentHash: newHash,
		OperationID: opID,
	}, nil
}
