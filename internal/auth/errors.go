package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrInvalidSession     = errors.New("会话无效或已过期")
	ErrInvalidToken       = errors.New("API token 无效")
	ErrSetupClosed        = errors.New("初始化已完成")
	ErrForbidden          = errors.New("权限不足")
	ErrCSRF               = errors.New("CSRF 校验失败")
)
