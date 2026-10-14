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
	if err := GetAclService().AddBlackIP(ctx, req.GetIp(), req.GetRemark(), req.GetOperator()); err != nil {
		tlog.Error(ctx, "grpc AddBlacklistIP failed ip:%s operator:%s err:%v", req.GetIp(), req.GetOperator(), err)
		if errors.Is(err, dmodel.ErrInvalidIP) {
			return nil, status.Error(codes.InvalidArgument, "invalid ip")
		}
		return nil, status.Error(codes.Internal, "add blacklist ip failed")
	}

	return &loginproto.AddBlacklistIPAck{Added: true}, nil
}

// AddBannedRole sgate 调用：封禁角色（落库 + 同步 Redis 缓存），登录与 token 校验时拦截
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

	return &loginproto.AddBannedRoleAck{Added: true}, nil
}

// RemoveBlacklistIP sgate 调用：移除黑名单（落库 + 同步 Redis 缓存）
func (h *LoginServer) RemoveBlacklistIP(
	ctx context.Context,
	req *loginproto.RemoveBlacklistIPReq,
) (*loginproto.RemoveBlacklistIPAck, error) {
	if err := GetAclService().RemoveBlackIP(ctx, req.GetIp(), req.GetOperator()); err != nil {
		tlog.Error(ctx, "grpc RemoveBlacklistIP failed ip:%s operator:%s err:%v", req.GetIp(), req.GetOperator(), err)
		if errors.Is(err, dmodel.ErrInvalidIP) {
			return nil, status.Error(codes.InvalidArgument, "invalid ip")
		}
		return nil, status.Error(codes.Internal, "remove blacklist ip failed")
	}

	return &loginproto.RemoveBlacklistIPAck{Removed: true}, nil
}

// RemoveBannedRole sgate 调用：解除角色封禁
func (h *LoginServer) RemoveBannedRole(
	ctx context.Context,
	req *loginproto.RemoveBannedRoleReq,
) (*loginproto.RemoveBannedRoleAck, error) {
	if err := GetAclService().RemoveBannedRole(ctx, req.GetRoleId(), req.GetOperator()); err != nil {
		tlog.Error(ctx, "grpc RemoveBannedRole failed roleId:%s operator:%s err:%v", req.GetRoleId(), req.GetOperator(), err)
		if errors.Is(err, dmodel.ErrInvalidRoleID) {
			return nil, status.Error(codes.InvalidArgument, "invalid role id")
		}
		return nil, status.Error(codes.Internal, "remove banned role failed")
	}

	return &loginproto.RemoveBannedRoleAck{Removed: true}, nil
}
