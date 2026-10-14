package service

import (
	"context"
	"errors"

	"github.com/streasure/loginserver/internal/dmodel"
	"github.com/streasure/util/tlog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	loginproto "github.com/streasure/protocol/loginserver"
)

// LoginServer 实现 loginproto.LoginServiceServer 接口，处理所有登录相关 gRPC 请求。
type LoginServer struct {
	loginproto.UnimplementedLoginServiceServer
}

func NewLoginServer() *LoginServer {
	return &LoginServer{}
}

func (h *LoginServer) ValidateLoginToken(
	ctx context.Context,
	req *loginproto.ValidateLoginTokenReq,
) (*loginproto.ValidateLoginTokenAck, error) {
	valid, err := GetLoginService().ValidateLoginToken(ctx, req.GetAccountId(), req.GetLoginToken())
	if err != nil {
		tlog.Error(ctx, "grpc ValidateLoginToken failed accountId:%s err:%v", req.GetAccountId(), err)
		return nil, status.Error(codes.Internal, "validate login token failed")
	}

	return &loginproto.ValidateLoginTokenAck{Valid: valid}, nil
}

// AddBlacklistIP sgate 调用：写入黑名单（落库 + 同步 Redis 缓存）
func (h *LoginServer) AddBlacklistIP(
	ctx context.Context,
	req *loginproto.AddBlacklistIPReq,
) (*loginproto.AddBlacklistIPAck, error) {
	if err := GetAclService().AddBlackIP(ctx, req.GetIp(), req.GetRemark()); err != nil {
		tlog.Error(ctx, "grpc AddBlacklistIP failed ip:%s operator:%s err:%v", req.GetIp(), req.GetOperator(), err)
		if errors.Is(err, dmodel.ErrInvalidIP) {
			return nil, status.Error(codes.InvalidArgument, "invalid ip")
		}
		return nil, status.Error(codes.Internal, "add blacklist ip failed")
	}

	tlog.Info(ctx, "grpc AddBlacklistIP success ip:%s operator:%s", req.GetIp(), req.GetOperator())
	return &loginproto.AddBlacklistIPAck{Added: true}, nil
}

// AddBannedRole sgate 调用：封禁角色（落库 + 同步 Redis 缓存），登录时拦截
func (h *LoginServer) AddBannedRole(
	ctx context.Context,
	req *loginproto.AddBannedRoleReq,
) (*loginproto.AddBannedRoleAck, error) {
	if err := GetAclService().AddBannedRole(ctx, req.GetRoleId(), req.GetReason(), req.GetOperator()); err != nil {
		tlog.Error(ctx, "grpc AddBannedRole failed roleId:%s operator:%s err:%v", req.GetRoleId(), req.GetOperator(), err)
		if errors.Is(err, dmodel.ErrInvalidRoleID) {
			return nil, status.Error(codes.InvalidArgument, "invalid role id")
		}
		return nil, status.Error(codes.Internal, "add banned role failed")
	}

	tlog.Info(ctx, "grpc AddBannedRole success roleId:%s operator:%s", req.GetRoleId(), req.GetOperator())
	return &loginproto.AddBannedRoleAck{Added: true}, nil
}
