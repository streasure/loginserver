package grpchandler

import (
	"context"
	"loginserver/internal/service"

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
	valid, err := service.GetLoginService().ValidateLoginToken(ctx, req.GetAccountId(), req.GetLoginToken())
	if err != nil {
		tlog.Error(ctx, "grpc ValidateLoginToken failed accountId:%s err:%v", req.GetAccountId(), err)
		return nil, status.Error(codes.Internal, "validate login token failed")
	}

	return &loginproto.ValidateLoginTokenAck{Valid: valid}, nil
}
