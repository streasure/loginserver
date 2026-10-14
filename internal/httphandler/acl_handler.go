package httphandler

import (
	"errors"
	"net"
	"net/http"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/dmodel"
	"github.com/streasure/loginserver/internal/pkg/dto"
	"github.com/streasure/loginserver/internal/service"

	"github.com/gin-gonic/gin"
	tlog "github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
	"github.com/streasure/util/uhttp"
)

// adminTokenHeader 管理接口鉴权请求头
const adminTokenHeader = "X-Admin-Token"

func init() {
	ugin.RegisterController("/api/v1/acl/whitelist", &ugin.HttpMapping{Method: http.MethodPost, Controller: AddWhitelistIP})
	ugin.RegisterController("/api/v1/acl/whitelist", &ugin.HttpMapping{Method: http.MethodDelete, Controller: DelWhitelistIP})
	ugin.RegisterController("/api/v1/acl/blacklist", &ugin.HttpMapping{Method: http.MethodPost, Controller: AddBlacklistIP})
	ugin.RegisterController("/api/v1/acl/blacklist", &ugin.HttpMapping{Method: http.MethodDelete, Controller: DelBlacklistIP})
	ugin.RegisterController("/api/v1/servers", &ugin.HttpMapping{Method: http.MethodGet, Controller: ListServers})
}

// checkAdminToken 校验管理接口 token（X-Admin-Token），失败写 401 并返回 false
func checkAdminToken(c *gin.Context) bool {
	if c.GetHeader(adminTokenHeader) != config.GetConfig().Acl.AdminToken {
		c.JSON(http.StatusUnauthorized, uhttp.Failure(dto.CodeUnauthorized, "unauthorized"))
		return false
	}
	return true
}

// aclOpFailed 统一写增删失败响应
func aclOpFailed(c *gin.Context, op string, err error) {
	tlog.Error(c.Request.Context(), "acl %s failed err:%v", op, err)
	if errors.Is(err, dmodel.ErrInvalidIP) {
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeParamError, "invalid ip"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, op+" failed"))
}

// AddWhitelistIP 新增白名单 IP：写 MySQL 并同步 Redis 缓存
func AddWhitelistIP(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclIPReq
	if !bindJSON(c, &req) {
		return
	}
	if net.ParseIP(req.IP) == nil {
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeParamError, "invalid ip"))
		return
	}
	if err := service.GetAclService().AddWhiteIP(c.Request.Context(), req.IP, req.Remark); err != nil {
		aclOpFailed(c, "add whitelist ip", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// DelWhitelistIP 删除白名单 IP
func DelWhitelistIP(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclIPDelReq
	if !bindJSON(c, &req) {
		return
	}
	if err := service.GetAclService().RemoveWhiteIP(c.Request.Context(), req.IP); err != nil {
		aclOpFailed(c, "delete whitelist ip", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// AddBlacklistIP 新增黑名单 IP：写 MySQL 并同步 Redis 缓存
func AddBlacklistIP(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclIPReq
	if !bindJSON(c, &req) {
		return
	}
	if net.ParseIP(req.IP) == nil {
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeParamError, "invalid ip"))
		return
	}
	if err := service.GetAclService().AddBlackIP(c.Request.Context(), req.IP, req.Remark); err != nil {
		aclOpFailed(c, "add blacklist ip", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// DelBlacklistIP 删除黑名单 IP
func DelBlacklistIP(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclIPDelReq
	if !bindJSON(c, &req) {
		return
	}
	if err := service.GetAclService().RemoveBlackIP(c.Request.Context(), req.IP); err != nil {
		aclOpFailed(c, "delete blacklist ip", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// ListServers 服务器列表（读缓存，无需管理 token）
func ListServers(c *gin.Context) {
	list, err := service.GetAclService().ServerList(c.Request.Context())
	if err != nil {
		tlog.Error(c.Request.Context(), "list servers failed err:%v", err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "list servers failed"))
		return
	}
	acks := make([]dto.ServerAck, 0, len(list))
	for _, s := range list {
		acks = append(acks, dto.ServerAck{
			ServerID:   s.ServerID,
			ServerName: s.ServerName,
			Zone:       s.Zone,
			IP:         s.IP,
			Port:       s.Port,
			Status:     s.Status,
			Sort:       s.Sort,
		})
	}
	c.JSON(http.StatusOK, uhttp.Success(acks))
}
