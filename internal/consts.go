package internal

import "errors"

const (
	LoginModeNormal = 0
	LoginModeSuper  = 1
)

const (
	ActionNormalLogin = "normalLogin"
	ActionGetVersion  = "getVersion"
)

var (
	ParamError             = errors.New("param error")
	ServerInternalError    = errors.New("server internal error")
	SDKLoginValidateFailed = errors.New("sdk login validate failed")
	RateLimitError         = errors.New("rate limit")
	AccountNotFound        = errors.New("account not found")
	LoginTokenInvalid      = errors.New("login token invalid")
	LoginTokenExpired      = errors.New("login token expired")
)
