package httphandler

import (
	"context"
	"crypto/subtle"
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

// checkAdminToken 校验管理接口 token（X-Admin-Token），失败写 401 并返回 false。
// 常数时间比较，防时序侧信道逐字节探测
func checkAdminToken(c *gin.Context) bool {
	got := c.GetHeader(adminTokenHeader)
	want := config.GetConfig().Acl.AdminToken
	// 空 token 一律拒绝（fail-closed）：配置校验虽要求必填，此处兜底防
	// 未加载/被清空时空串与缺失头比对相等而放行（sgate admin 同口径空即拒）。
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
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

// handleAdminMutate 管理接口写操作公共段：token 鉴权 → 绑定请求体 → 执行 → 统一响应。
// fn 返回 ErrInvalidIP 按参数错误响应，其余按操作失败响应。
func handleAdminMutate[T any](c *gin.Context, op string, fn func(ctx context.Context, req *T) error) {
	if !checkAdminToken(c) {
		return
	}
	var req T
	if !bindJSON(c, &req) {
		return
	}
	if err := fn(c.Request.Context(), &req); err != nil {
		aclOpFailed(c, op, err)
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(true))
}

// handleAdminList 管理接口列表公共段：token 鉴权 → 解析 limit → 拉取 → 统一响应。
func handleAdminList[T any](c *gin.Context, name string, fn func(ctx context.Context, limit int) ([]T, error)) {
	if !checkAdminToken(c) {
		return
	}
	rows, err := fn(c.Request.Context(), listLimit(c, aclListDefaultLimit, aclListMaxLimit))
	if err != nil {
		tlog.Error(c.Request.Context(), "acl %s failed err:%v", name, err)
		c.JSON(http.StatusOK, uhttp.Failure(dto.CodeAclFailed, name+" failed"))
		return
	}
	c.JSON(http.StatusOK, uhttp.Success(rows))
}

// AddWhitelistIP 新增白名单 IP（支持纯 IP 与 CIDR）
func AddWhitelistIP(c *gin.Context) {
	handleAdminMutate(c, "add whitelist ip", func(ctx context.Context, req *dto.AclIPReq) error {
		return service.GetAclService().AddWhiteIP(ctx, req.IP, req.Remark, req.Operator)
	})
}

// DelWhitelistIP 删除白名单 IP
func DelWhitelistIP(c *gin.Context) {
	handleAdminMutate(c, "delete whitelist ip", func(ctx context.Context, req *dto.AclIPDelReq) error {
		return service.GetAclService().RemoveWhiteIP(ctx, req.IP, req.Operator)
	})
}

// AddBlacklistIP 新增黑名单 IP（支持纯 IP 与 CIDR）：落库后 best-effort 踢在线连接
func AddBlacklistIP(c *gin.Context) {
	handleAdminMutate(c, "add blacklist ip", func(ctx context.Context, req *dto.AclIPReq) error {
		return service.GetAclService().AddBlackIP(ctx, req.IP, req.Remark, req.Operator)
	})
}

// DelBlacklistIP 删除黑名单 IP
func DelBlacklistIP(c *gin.Context) {
	handleAdminMutate(c, "delete blacklist ip", func(ctx context.Context, req *dto.AclIPDelReq) error {
		return service.GetAclService().RemoveBlackIP(ctx, req.IP, req.Operator)
	})
}

// AddBannedRole 封禁角色：落库后 best-effort 踢该账号在线连接
func AddBannedRole(c *gin.Context) {
	handleAdminMutate(c, "add banned role", func(ctx context.Context, req *dto.AclBannedRoleReq) error {
		return service.GetAclService().AddBannedRole(ctx, req.RoleId, req.Reason, req.Operator)
	})
}

// DelBannedRole 解除角色封禁
func DelBannedRole(c *gin.Context) {
	handleAdminMutate(c, "remove banned role", func(ctx context.Context, req *dto.AclBannedRoleDelReq) error {
		return service.GetAclService().RemoveBannedRole(ctx, req.RoleId, req.Operator)
	})
}

// listLimit 解析 ?limit=（缺省/非法/非正用 def，超上限钳到 max）。
// ACL 名单与会话扫描列表共用同一解析语义。
func listLimit(c *gin.Context, def, max int) int {
	v, err := strconv.Atoi(c.Query("limit"))
	if err != nil || v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// ListWhitelistIPs 白名单列表（直查 MySQL）
func ListWhitelistIPs(c *gin.Context) {
	handleAdminList(c, "list whitelist", service.GetAclService().ListWhiteIPs)
}

// ListBlacklistIPs 黑名单列表
func ListBlacklistIPs(c *gin.Context) {
	handleAdminList(c, "list blacklist", service.GetAclService().ListBlackIPs)
}

// ListBannedRoles 封禁角色列表
func ListBannedRoles(c *gin.Context) {
	handleAdminList(c, "list banned roles", service.GetAclService().ListBannedRoles)
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
