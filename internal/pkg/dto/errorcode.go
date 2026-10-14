package dto

// 业务错误码
const (
	CodeParamError        = 1001
	CodeBindAccountFailed = 1002
	CodeTokenGenFailed    = 1003
	CodeIpBlocked         = 1004 // 登录 IP 命中黑名单
	CodeRoleBanned        = 1005 // 角色被封禁
	CodeUnauthorized      = 1006 // 管理接口 token 校验失败
	CodeAclFailed         = 1007 // 黑白名单/封禁检测或增删失败
)
