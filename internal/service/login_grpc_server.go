package service

import (
	"context"

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
		// 记录最近登录服务器（登录 UI 推荐用，30天 TTL）
		if req.GetServerId() != "" {
			if err := rmodel.SetLastLoginServer(ctx, req.GetAccountId(), req.GetServerId()); err != nil {
				tlog.Warn(ctx, "set last login server failed accountId:%s serverId:%s err:%v",
					req.GetAccountId(), req.GetServerId(), err)
			}
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
	// 批量预取：进程内缓存 miss 的 token 一次 MGET 合并回源。
	// 不预取时 500 条 miss 即 500 次串行 GET，叠加 Redis 超时可把单个 RPC 拖到秒级
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.GetAccountId()
	}
	GetLoginService().PrefetchLoginTokens(ctx, ids)
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
