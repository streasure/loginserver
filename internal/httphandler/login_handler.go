package httphandler

import (
	"errors"
	"net/http"

	"github.com/streasure/loginserver/internal"
	"github.com/streasure/loginserver/internal/pkg/dto"
	"github.com/streasure/loginserver/internal/service"

	"github.com/gin-gonic/gin"
	tlog "github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
	"github.com/streasure/util/uhttp"
)

// 注册所有 HTTP 路由到 ugin 全局路由表
func init() {
	ugin.RegisterController("/api/v1/login", &ugin.HttpMapping{Method: http.MethodPost, Controller: Login})
	ugin.RegisterController("/api/v1/version", &ugin.HttpMapping{Method: http.MethodPost, Controller: GetVersion})
	ugin.RegisterController("/api/v1/validate/token", &ugin.HttpMapping{Method: http.MethodPost, Controller: ValidateLoginToken})
}

// bindJSON 绑定并校验 JSON 请求体；失败时记录日志、写入参数错误响应并返回 false
func bindJSON[T any](c *gin.Context, req *T) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		tlog.Warn(c.Request.Context(), "bind request json failed path:%s err:%v", c.FullPath(), err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeParamError, "param error"))
		return false
	}
	return true
}

func Login(c *gin.Context) {
	ctx := c.Request.Context()

	var loginReq dto.LoginReq
	if !bindJSON(c, &loginReq) {
		return
	}

	// 登录前 IP 黑白名单检测：白名单放行（跳过黑名单），命中黑名单直接拒绝
	clientIP := c.ClientIP()
	if err := service.GetAclService().CheckLoginIP(ctx, clientIP); err != nil {
		if errors.Is(err, service.ErrIPBlacklisted) {
			tlog.Warn(ctx, "login blocked, ip:%s in blacklist openId:%s", clientIP, loginReq.OpenId)
			c.JSON(http.StatusOK, uhttp.Failure(dto.CodeIpBlocked, "ip blocked"))
			return
		}
		tlog.Error(ctx, "acl check ip:%s openId:%s failed err:%v", clientIP, loginReq.OpenId, err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "acl check failed"))
		return
	}

	loginService := service.GetLoginService()

	accountId, err := loginService.BindAccount(ctx, loginReq.OpenId, loginReq.PtId)
	if err != nil || len(accountId) == 0 {
		tlog.Error(ctx, "bind account failed openId:%s ptId:%d err:%v", loginReq.OpenId, loginReq.PtId, err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeBindAccountFailed, "bind account failed"))
		return
	}

	// 封禁角色检测：命中则拒绝发 token
	if err := service.GetAclService().CheckRole(ctx, accountId); err != nil {
		if errors.Is(err, service.ErrRoleBanned) {
			tlog.Warn(ctx, "login blocked, account:%s banned", accountId)
			c.JSON(http.StatusOK, uhttp.Failure(dto.CodeRoleBanned, "role banned"))
			return
		}
		tlog.Error(ctx, "acl check account:%s failed err:%v", accountId, err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "acl check failed"))
		return
	}

	loginToken, err := loginService.GenerateLoginToken(ctx, accountId)
	if err != nil {
		tlog.Error(ctx, "generate account:%s login token err:%v", accountId, err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeTokenGenFailed, "generate login token failed"))
		return
	}

	tlog.Info(ctx, "login success account:%s token:%s", accountId, loginToken)

	c.JSON(http.StatusOK, uhttp.Success(dto.LoginAck{
		AccountId:  accountId,
		LoginToken: loginToken,
	}))
}

func GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, uhttp.Success(dto.GetVersionAck{
		LoginServerVersion: internal.Version,
	}))
}

func ValidateLoginToken(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.ValidateLoginTokenReq
	if !bindJSON(c, &req) {
		return
	}

	loginService := service.GetLoginService()

	valid, err := loginService.ValidateLoginToken(ctx, req.AccountId, req.LoginToken)
	if err != nil {
		tlog.Error(ctx, "validate account:%s login token:%s error:%v", req.AccountId, req.LoginToken, err)
		c.JSON(http.StatusInternalServerError, uhttp.Failure(dto.CodeTokenGenFailed, "validate login token failed"))
		return
	}

	tlog.Info(ctx, "validate account:%s login token:%s ok", req.AccountId, req.LoginToken)

	c.JSON(http.StatusOK, uhttp.Success(dto.ValidateLoginTokenAck{
		Valid: valid,
	}))
}
