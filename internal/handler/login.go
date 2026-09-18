package handler

import (
	"loginserver/internal/config"
	"net/http"
	"sync"

	"loginserver/internal"
	"loginserver/internal/pkg/dto"
	"loginserver/internal/service"

	"github.com/gin-gonic/gin"
	tlog "github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
)

var (
	// loginServiceOnce 保证 LoginService 只构造一次，
	// 避免每个 HTTP 请求重复创建实例和重复计算 key 前缀
	loginServiceOnce sync.Once
	loginService     *service.LoginService
)

// getLoginService 返回进程级 LoginService 单例。
// 首次调用发生在 HTTP 服务启动后，此时配置已加载完成
func getLoginService() *service.LoginService {
	loginServiceOnce.Do(func() {
		loginService = service.NewLoginService(config.GetConfig())
	})
	return loginService
}

func init() {
	ugin.RegisterController("/api/v1/login", &ugin.HttpMapping{Method: http.MethodPost, Controller: Login})
	ugin.RegisterController("/api/v1/server/list", &ugin.HttpMapping{Method: http.MethodPost, Controller: GetServerList})
	ugin.RegisterController("/api/v1/version", &ugin.HttpMapping{Method: http.MethodPost, Controller: GetVersion})
	ugin.RegisterController("/api/v1/validate/token", &ugin.HttpMapping{Method: http.MethodPost, Controller: ValidateLoginToken})
}

func Login(c *gin.Context) {
	ctx := c.Request.Context()

	var loginReq dto.LoginReq
	if err := c.ShouldBindJSON(&loginReq); err != nil {
		tlog.Info(ctx, "login param error", "error", err.Error())
		c.JSON(http.StatusOK, dto.Failure(1001, "param error"))
		return
	}

	cfg := config.GetConfig()
	loginService := getLoginService()

	accountId, err := loginService.BindAccount(ctx, loginReq.OpenId, loginReq.PtId)
	if err != nil || len(accountId) == 0 {
		tlog.Error(ctx, "bind account failed",
			"openId", loginReq.OpenId,
			"ptId", loginReq.PtId,
			"error", err,
		)
		c.JSON(http.StatusOK, dto.Failure(1002, "bind account failed"))
		return
	}

	loginToken, err := loginService.GenerateLoginToken(ctx, accountId)
	if err != nil {
		tlog.Error(ctx, "generate login token failed",
			"accountId", accountId,
			"error", err.Error(),
		)
		c.JSON(http.StatusOK, dto.Failure(1003, "generate login token failed"))
		return
	}

	tlog.Info(ctx, "login success",
		"accountId", accountId,
		"openId", loginReq.OpenId,
	)

	c.JSON(http.StatusOK, dto.Success(dto.LoginAck{
		AccountId:     accountId,
		LoginToken:    loginToken,
		ServerListUrl: cfg.ServerList.ClientGetServerListUrl,
	}))
}

func GetServerList(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.GetServerListReq
	if err := c.ShouldBindJSON(&req); err != nil {
		tlog.Info(ctx, "get server list param error", "error", err.Error())
		c.JSON(http.StatusOK, dto.Failure(1001, "param error"))
		return
	}

	loginService := getLoginService()

	valid, err := loginService.ValidateLoginToken(ctx, req.AccountId, req.LoginToken)
	if err != nil || !valid {
		tlog.Info(ctx, "login token invalid",
			"accountId", req.AccountId,
			"error", err,
		)
		c.JSON(http.StatusOK, dto.Failure(1004, "login token invalid"))
		return
	}

	servers := loginService.GetServerList(ctx)
	c.JSON(http.StatusOK, dto.Success(dto.GetServerListAck{
		Servers: servers,
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
		c.JSON(http.StatusOK, dto.Failure(1001, "param error"))
		return
	}

	loginService := getLoginService()

	valid, err := loginService.ValidateLoginToken(ctx, req.AccountId, req.LoginToken)
	if err != nil {
		tlog.Info(ctx, "validate login token error",
			"accountId", req.AccountId,
			"error", err.Error(),
		)
		c.JSON(http.StatusOK, dto.Success(dto.ValidateLoginTokenAck{
			Valid: false,
		}))
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
