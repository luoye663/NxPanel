// auth 包 — 错误定义
//
// 定义认证模块的所有业务错误。
// handler 层根据这些错误决定返回什么 HTTP 状态码和错误码。
package auth

import "errors"

// 认证相关错误
	var (
	ErrAdminAlreadyExists = errors.New("管理员已初始化")

	ErrInvalidCredentials = errors.New("用户名或密码错误")

	ErrUsernameRequired = errors.New("用户名不能为空")

	ErrUsernameTooLong = errors.New("用户名不能超过 50 个字符")

	ErrPasswordRequired = errors.New("密码不能为空")

	ErrPasswordTooShort = errors.New("密码长度不能少于 8 个字符")

	ErrPasswordTooLong = errors.New("密码长度不能超过 72 个字符")

	ErrPasswordTooWeak = errors.New("密码强度不足，需包含大小写字母、数字、特殊字符中的至少 3 种")

	ErrSessionNotFound = errors.New("会话不存在或已过期")

	ErrSessionMismatch = errors.New("会话环境变更，请重新登录")

	ErrCSRFInvalid = errors.New("CSRF Token 验证失败")

	ErrInvalidTOTP = errors.New("验证码错误")

	ErrTOTPCodeReplayed = errors.New("该验证码已被使用，请等待新验证码")

	ErrInvalidRecoveryCode = errors.New("恢复码错误或已使用")

	Err2FANotEnabled = errors.New("两步验证未启用")

	Err2FAAlreadyEnabled = errors.New("两步验证已启用")

	ErrTempTokenInvalid = errors.New("临时令牌无效或已过期")
)
