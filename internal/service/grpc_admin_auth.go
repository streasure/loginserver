package service

import (
	"context"

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
	if len(got) == 0 || got[0] != config.GetConfig().Acl.AdminToken {
		return nil, status.Error(codes.Unauthenticated, "admin token required")
	}
	return handler(ctx, req)
}
