package httphandler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/streasure/loginserver/internal/config"
)

// TestCheckAdminTokenFailClosed 管理接口鉴权 fail-closed 回归：
// 配置 token 为空时无论是否携带请求头一律拒绝（防空串与缺失头比对相等放行）；
// 非空时缺失/错误头拒绝、正确头放行。
func TestCheckAdminTokenFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := config.GetConfig().Acl.AdminToken
	defer func() { config.GetConfig().Acl.AdminToken = old }()

	newCtx := func(withHeader bool) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/acl/blacklist", nil)
		if withHeader {
			req.Header.Set(adminTokenHeader, "secret")
		}
		c.Request = req
		return c
	}

	// 空配置 token：带不带（哪怕“正确”）头都拒绝
	config.GetConfig().Acl.AdminToken = ""
	if checkAdminToken(newCtx(false)) {
		t.Error("empty configured token without header must be rejected")
	}
	if checkAdminToken(newCtx(true)) {
		t.Error("empty configured token with header must be rejected")
	}

	// 非空配置：缺失头/正确头
	config.GetConfig().Acl.AdminToken = "secret"
	if checkAdminToken(newCtx(false)) {
		t.Error("missing header must be rejected")
	}
	if !checkAdminToken(newCtx(true)) {
		t.Error("matching header must pass")
	}
}
