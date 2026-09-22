package handler

import (
	"net/http"

	"loginserver/internal"
	"loginserver/internal/pkg/dto"
	"loginserver/internal/service"

	"github.com/gin-gonic/gin"
	tlog "github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
)

// 注册所有 HTTP 路由到 ugin 全局路由表
func init() {
	ugin.RegisterController("/api/v1/login", &ugin.HttpMapping{Method: http.MethodPost, Controller: Login})
	ugin.RegisterController("/api/v1/version", &ugin.HttpMapping{Method: http.MethodPost, Controller: GetVersion})
	ugin.RegisterController("/api/v1/validate/token", &ugin.HttpMapping{Method: http.MethodPost, Controller: ValidateLoginToken})
}

func Login(c *gin.Context) {
	ctx := c.Request.Context()

	var loginReq dto.LoginReq
	if err := c.ShouldBindJSON(&loginReq); err != nil {
		tlog.Info(ctx, "login param error", "error", err.Error())
		c.JSON(http.StatusOK, dto.Failure(dto.CodeParamError, "param error"))
		return
	}

	loginService := service.GetLoginService()

	accountId, err := loginService.BindAccount(ctx, loginReq.OpenId, loginReq.PtId)
	if err != nil || len(accountId) == 0 {
		tlog.Error(ctx, "bind account failed",
			"openId", loginReq.OpenId,
			"ptId", loginReq.PtId,
			"error", err,
		)
		c.JSON(http.StatusOK, dto.Failure(dto.CodeBindAccountFailed, "bind account failed"))
		return
	}

	loginToken, err := loginService.GenerateLoginToken(ctx, accountId)
	if err != nil {
		tlog.Error(ctx, "generate login token failed",
			"accountId", accountId,
			"error", err.Error(),
		)
		c.JSON(http.StatusOK, dto.Failure(dto.CodeTokenGenFailed, "generate login token failed"))
		return
	}

	tlog.Info(ctx, "login success",
		"accountId", accountId,
		"openId", loginReq.OpenId,
	)

	c.JSON(http.StatusOK, dto.Success(dto.LoginAck{
		AccountId:  accountId,
		LoginToken: loginToken,
	}))
}

func GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, dto.Success(dto.GetVersionAck{
		LoginServerVersion: internal.Version,
	}))
}

func ValidateLoginToken(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.ValidateLoginTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		tlog.Info(ctx, "validate login token param error", "error", err.Error())
		c.JSON(http.StatusOK, dto.Failure(dto.CodeParamError, "param error"))
		return
	}

	loginService := service.GetLoginService()

	valid, err := loginService.ValidateLoginToken(ctx, req.AccountId, req.LoginToken)
	if err != nil {
		tlog.Error(ctx, "validate login token error",
			"accountId", req.AccountId,
			"error", err.Error(),
		)
		c.JSON(http.StatusInternalServerError, dto.Failure(dto.CodeTokenGenFailed, "validate login token failed"))
		return
	}

	tlog.Debug(ctx, "validate login token",
		"accountId", req.AccountId,
		"valid", valid,
	)

	c.JSON(http.StatusOK, dto.Success(dto.ValidateLoginTokenAck{
		Valid: valid,
	}))
}
