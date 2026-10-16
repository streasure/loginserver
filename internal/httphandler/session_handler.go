package httphandler

import (
	"net/http"
	"strconv"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/pkg/dto"
	"github.com/streasure/loginserver/internal/rmodel"

	"github.com/gin-gonic/gin"
	"github.com/streasure/util/ugin"
	"github.com/streasure/util/uhttp"
)

func init() {
	ugin.RegisterController("/api/v1/sessions", &ugin.HttpMapping{Method: http.MethodGet, Controller: ListSessions})
	ugin.RegisterController("/api/v1/sessions/account", &ugin.HttpMapping{Method: http.MethodGet, Controller: ListAccountSessions})
}

// sessionListMaxLimit 扫码列表上限（防无界 SCAN）
const sessionListMaxLimit = 500

// ListSessions 全局会话列表（SCAN 扫码模式，运维/admin 面板用）。
// 查询参数：limit（默认 200，最大 500）、sgateId（可选过滤）。
// 返回 sess:conn hash 的摘要列表（不含 token/loginKey 等敏感字段）。
func ListSessions(c *gin.Context) {
	if c.GetHeader(adminTokenHeader) != config.GetConfig().Acl.AdminToken {
		c.JSON(http.StatusUnauthorized, uhttp.Failure(dto.CodeUnauthorized, "unauthorized"))
		return
	}
	limit := 200
	if s := c.Query("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > sessionListMaxLimit {
		limit = sessionListMaxLimit
	}
	sgateFilter := c.Query("sgateId")

	sessions, err := rmodel.ListSessions(c.Request.Context(), limit, sgateFilter)
	if err != nil {
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "list sessions failed"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(sessions))
}

// ListAccountSessions 查询指定账号的在线会话（踢人路由反查/运维）。
func ListAccountSessions(c *gin.Context) {
	if c.GetHeader(adminTokenHeader) != config.GetConfig().Acl.AdminToken {
		c.JSON(http.StatusUnauthorized, uhttp.Failure(dto.CodeUnauthorized, "unauthorized"))
		return
	}
	accountID := c.Query("accountId")
	if accountID == "" {
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeParamError, "accountId required"))
		return
	}
	sgates, err := rmodel.SessionSgatesByAccount(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "lookup failed"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(gin.H{"accountId": accountID, "sgates": sgates}))
}
