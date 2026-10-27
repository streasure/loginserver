package service

import (
	"context"
	"crypto/subtle"

	"github.com/streasure/loginserver/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// adminTokenMetadataKey 管理类 RPC 的鉴权元数据键（gRPC metadata 键必须小写）
const adminTokenMetadataKey = "x-admin-token"

// grpcHealthMethods 健康检查方法（服务发现/探活路径，免管理鉴权）
var openMethods = map[string]struct{}{
	"/loginserver.LoginService/ValidateLoginToken":       {}, // 登录公开路径，由 loginToken 本身鉴权
	"/loginserver.LoginService/BatchValidateLoginTokens": {},
	"/grpc.health.v1.Health/Check":                       {},
	"/grpc.health.v1.Health/Watch":                       {},
}

// AdminAuthUnaryInterceptor 管理类 RPC 鉴权拦截器：非开放方法必须携带
// metadata x-admin-token 且与配置 acl.adminToken 一致
// （env LOGINSERVER_ACL_ADMIN_TOKEN 可覆盖 yaml）。
// 经 ugrpc.WithGrpcOptions(grpc.ChainUnaryInterceptor(...)) 注入，不改 util。
func AdminAuthUnaryInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if _, open := openMethods[info.FullMethod]; open {
		return handler(ctx, req)
	}
	md, _ := metadata.FromIncomingContext(ctx)
	got := md.Get(adminTokenMetadataKey)
	// 常数时间比较，防时序侧信道逐字节探测；
	// 配置 token 为空一律拒绝（fail-closed，防空值与空配置比对相等放行）
	adminToken := config.GetConfig().Acl.AdminToken
	if adminToken == "" || len(got) == 0 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(adminToken)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "admin token required")
	}
	return handler(ctx, req)
}
