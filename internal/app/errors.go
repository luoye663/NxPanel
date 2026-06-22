// app 包的错误定义
// 定义面板统一的业务错误码，与 API 响应中的 error.code 对应
package app

// 错误码常量
const (
	ErrBadRequest        = "BAD_REQUEST"
	ErrUnauthorized      = "UNAUTHORIZED"
	ErrForbidden         = "FORBIDDEN"
	ErrNotFound          = "NOT_FOUND"
	ErrConflict          = "CONFLICT"
	ErrValidationFailed  = "VALIDATION_FAILED"
	ErrConfigDrifted     = "CONFIG_DRIFTED"
	ErrNginxTestFailed   = "NGINX_TEST_FAILED"
	ErrNginxReloadFailed = "NGINX_RELOAD_FAILED"
	ErrAgentUnavailable  = "AGENT_UNAVAILABLE"
	ErrAgentDenied       = "AGENT_DENIED"
	ErrInternalError     = "INTERNAL_ERROR"
)

// AppError 表示一个业务错误
type AppError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	return e.Message
}

// NewAppError 创建业务错误
func NewAppError(code, message string, details map[string]any) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// 常用错误构造函数

func ErrBadRequestMsg(msg string) *AppError {
	return NewAppError(ErrBadRequest, msg, nil)
}

func ErrUnauthorizedMsg(msg string) *AppError {
	return NewAppError(ErrUnauthorized, msg, nil)
}

func ErrNotFoundMsg(msg string) *AppError {
	return NewAppError(ErrNotFound, msg, nil)
}

func ErrConflictMsg(msg string) *AppError {
	return NewAppError(ErrConflict, msg, nil)
}

func ErrValidationFailedMsg(msg string, details map[string]any) *AppError {
	return NewAppError(ErrValidationFailed, msg, details)
}

func ErrNginxTestFailedMsg(stderr string) *AppError {
	return NewAppError(ErrNginxTestFailed, "Nginx 配置测试失败", map[string]any{
		"stderr": stderr,
	})
}

func ErrNginxReloadFailedMsg(stderr string) *AppError {
	return NewAppError(ErrNginxReloadFailed, "Nginx reload 失败", map[string]any{
		"stderr": stderr,
	})
}

func ErrAgentUnavailableMsg(msg string) *AppError {
	return NewAppError(ErrAgentUnavailable, msg, nil)
}
