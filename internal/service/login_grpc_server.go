package service

import (
	"context"
	"errors"

	"github.com/streasure/loginserver/internal/dmodel"
	"github.com/streasure/loginserver/internal/rmodel"
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

	// 会话索引（单向 Kick 模型）：sgate 过门校验携带会话上下文时写三键缓存；
	// best-effort，写失败不影响校验结果。BatchValidate 复用本消息但不带上下文，天然跳过
	if valid && req.GetSgateId() != "" && req.GetConnId() != "" {
		if err := rmodel.WriteSession(ctx, req.GetSgateId(), req.GetConnId(), req.GetAccountId(), req.GetClientIp()); err != nil {
			tlog.Warn(ctx, "write session cache failed accountId:%s sgate:%s conn:%s err:%v",
				req.GetAccountId(), req.GetSgateId(), req.GetConnId(), err)
		}
	}

	return &loginproto.ValidateLoginTokenAck{Valid: valid}, nil
}

// maxBatchValidateItems 单次批量校验上限（sgate 周期重校验分片建议远小于此值）
const maxBatchValidateItems = 500

// BatchValidateLoginTokens sgate 调用：批量校验在线会话 token（封禁/失效会话周期回收）。
// 结果与 items 顺序一一对应；单条失败按 invalid 处理（不因个别失败中断整批）
func (h *LoginServer) BatchValidateLoginTokens(
	ctx context.Context,
	req *loginproto.BatchValidateLoginTokensReq,
) (*loginproto.BatchValidateLoginTokensAck, error) {
	items := req.GetItems()
	if len(items) > maxBatchValidateItems {
		return nil, status.Errorf(codes.InvalidArgument, "batch size %d exceeds limit %d", len(items), maxBatchValidateItems)
	}
	valid := make([]bool, len(items))
	for i, item := range items {
		v, err := GetLoginService().ValidateLoginToken(ctx, item.GetAccountId(), item.GetLoginToken())
		if err != nil {
			tlog.Error(ctx, "grpc BatchValidateLoginTokens item failed accountId:%s err:%v", item.GetAccountId(), err)
			v = false
		}
		valid[i] = v
	}
	return &loginproto.BatchValidateLoginTokensAck{Valid: valid}, nil
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
