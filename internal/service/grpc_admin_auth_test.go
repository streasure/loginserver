package service

import (
	"context"
	"testing"

	"github.com/streasure/loginserver/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestAdminAuthUnaryInterceptor(t *testing.T) {
	old := config.GetConfig().Acl.AdminToken
	config.GetConfig().Acl.AdminToken = "test-token"
	defer func() { config.GetConfig().Acl.AdminToken = old }()

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/loginserver.LoginService/AddBlacklistIP"}

	// 无 metadata → 拒绝
	if _, err := AdminAuthUnaryInterceptor(context.Background(), nil, info, handler); err == nil {
		t.Error("missing metadata should be rejected")
	}

	// 错误 token → 拒绝
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(adminTokenMetadataKey, "wrong"))
	if _, err := AdminAuthUnaryInterceptor(ctx, nil, info, handler); err == nil {
		t.Error("wrong token should be rejected")
	}

	// 正确 token → 放行
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs(adminTokenMetadataKey, "test-token"))
	v, err := AdminAuthUnaryInterceptor(ctx, nil, info, handler)
	if err != nil || v != "ok" {
		t.Errorf("valid token: v=%v err=%v", v, err)
	}

	// 开放方法（登录校验/健康检查）无 token 放行
	open := &grpc.UnaryServerInfo{FullMethod: "/loginserver.LoginService/ValidateLoginToken"}
	if _, err := AdminAuthUnaryInterceptor(context.Background(), nil, open, handler); err != nil {
		t.Errorf("open method should pass without token: %v", err)
	}
}

// TestAdminAuthEmptyTokenFailClosed 配置 token 为空时一律拒绝（fail-closed）：
// 空配置与空 metadata 值比对相等也不得放行。
func TestAdminAuthEmptyTokenFailClosed(t *testing.T) {
	old := config.GetConfig().Acl.AdminToken
	config.GetConfig().Acl.AdminToken = ""
	defer func() { config.GetConfig().Acl.AdminToken = old }()

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/loginserver.LoginService/AddBlacklistIP"}

	if _, err := AdminAuthUnaryInterceptor(context.Background(), nil, info, handler); err == nil {
		t.Error("empty configured token must be rejected (no metadata)")
	}
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(adminTokenMetadataKey, ""))
	if _, err := AdminAuthUnaryInterceptor(ctx, nil, info, handler); err == nil {
		t.Error("empty configured token must be rejected (empty metadata value)")
	}
}
