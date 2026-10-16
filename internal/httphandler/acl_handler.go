package httphandler

import (
	"errors"
	"net/http"
	"strconv"

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

// aclListDefaultLimit / aclListMaxLimit 名单列表接口默认/最大条数（直查 MySQL，防止无界查询）
const (
	aclListDefaultLimit = 1000
	aclListMaxLimit     = 1000
)

func init() {
	ugin.RegisterController("/api/v1/acl/whitelist", &ugin.HttpMapping{Method: http.MethodPost, Controller: AddWhitelistIP})
	ugin.RegisterController("/api/v1/acl/whitelist", &ugin.HttpMapping{Method: http.MethodDelete, Controller: DelWhitelistIP})
	ugin.RegisterController("/api/v1/acl/whitelist", &ugin.HttpMapping{Method: http.MethodGet, Controller: ListWhitelistIPs})
	ugin.RegisterController("/api/v1/acl/blacklist", &ugin.HttpMapping{Method: http.MethodPost, Controller: AddBlacklistIP})
	ugin.RegisterController("/api/v1/acl/blacklist", &ugin.HttpMapping{Method: http.MethodDelete, Controller: DelBlacklistIP})
	ugin.RegisterController("/api/v1/acl/blacklist", &ugin.HttpMapping{Method: http.MethodGet, Controller: ListBlacklistIPs})
	ugin.RegisterController("/api/v1/acl/banned-role", &ugin.HttpMapping{Method: http.MethodPost, Controller: AddBannedRole})
	ugin.RegisterController("/api/v1/acl/banned-role", &ugin.HttpMapping{Method: http.MethodDelete, Controller: DelBannedRole})
	ugin.RegisterController("/api/v1/acl/banned-role", &ugin.HttpMapping{Method: http.MethodGet, Controller: ListBannedRoles})
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

// AddWhitelistIP 新增白名单 IP（支持纯 IP 与 CIDR）：写 MySQL 并同步 Redis 缓存
func AddWhitelistIP(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclIPReq
	if !bindJSON(c, &req) {
		return
	}
	if err := service.GetAclService().AddWhiteIP(c.Request.Context(), req.IP, req.Remark, req.Operator); err != nil {
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
	if err := service.GetAclService().RemoveWhiteIP(c.Request.Context(), req.IP, req.Operator); err != nil {
		aclOpFailed(c, "delete whitelist ip", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// AddBlacklistIP 新增黑名单 IP（支持纯 IP 与 CIDR）：写 MySQL 并同步 Redis 缓存
func AddBlacklistIP(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclIPReq
	if !bindJSON(c, &req) {
		return
	}
	if err := service.GetAclService().AddBlackIP(c.Request.Context(), req.IP, req.Remark, req.Operator); err != nil {
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
	if err := service.GetAclService().RemoveBlackIP(c.Request.Context(), req.IP, req.Operator); err != nil {
		aclOpFailed(c, "delete blacklist ip", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// AddBannedRole 封禁角色（HTTP 管理入口，与 sgate gRPC 入口同底层）
func AddBannedRole(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclBannedRoleReq
	if !bindJSON(c, &req) {
		return
	}
	if err := service.GetAclService().AddBannedRole(c.Request.Context(), req.RoleId, req.Reason, req.Operator); err != nil {
		aclOpFailed(c, "add banned role", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// DelBannedRole 解除角色封禁
func DelBannedRole(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	var req dto.AclBannedRoleDelReq
	if !bindJSON(c, &req) {
		return
	}
	if err := service.GetAclService().RemoveBannedRole(c.Request.Context(), req.RoleId, req.Operator); err != nil {
		aclOpFailed(c, "remove banned role", err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// aclListLimit 解析 ?limit=（默认/上限 1000；非法按默认）
func aclListLimit(c *gin.Context) int {
	v := c.Query("limit")
	if v == "" {
		return aclListDefaultLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return aclListDefaultLimit
	}
	if n > aclListMaxLimit {
		return aclListMaxLimit
	}
	return n
}

// ListWhitelistIPs 白名单列表（直查 MySQL，需管理 token）
func ListWhitelistIPs(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	rows, err := service.GetAclService().ListWhiteIPs(c.Request.Context(), aclListLimit(c))
	if err != nil {
		tlog.Error(c.Request.Context(), "list whitelist failed err:%v", err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "list whitelist failed"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(rows))
}

// ListBlacklistIPs 黑名单列表
func ListBlacklistIPs(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	rows, err := service.GetAclService().ListBlackIPs(c.Request.Context(), aclListLimit(c))
	if err != nil {
		tlog.Error(c.Request.Context(), "list blacklist failed err:%v", err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "list blacklist failed"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(rows))
}

// ListBannedRoles 封禁角色列表
func ListBannedRoles(c *gin.Context) {
	if !checkAdminToken(c) {
		return
	}
	rows, err := service.GetAclService().ListBannedRoles(c.Request.Context(), aclListLimit(c))
	if err != nil {
		tlog.Error(c.Request.Context(), "list banned roles failed err:%v", err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, "list banned roles failed"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(rows))
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
