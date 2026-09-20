package rpc

import (
	"context"

	"github.com/streasure/util/tlog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	loginproto "github.com/streasure/protocol/loginserver"

	"loginserver/internal/service"
)

// LoginHandler 实现 loginproto.LoginServiceServer 接口，处理所有登录相关 gRPC 请求。
// 与 LoginGrpcServer 分离：LoginGrpcServer 负责服务器生命周期，LoginHandler 负责业务逻辑。
type LoginHandler struct {
	loginproto.UnimplementedLoginServiceServer
	loginService *service.LoginService
}

func NewLoginHandler(loginService *service.LoginService) *LoginHandler {
	return &LoginHandler{loginService: loginService}
}

func (h *LoginHandler) ValidateLoginToken(ctx context.Context, req *loginproto.ValidateLoginTokenReq) (*loginproto.ValidateLoginTokenAck, error) {
	valid, err := h.loginService.ValidateLoginToken(ctx, req.GetAccountId(), req.GetLoginToken())
	if err != nil {
		tlog.Error(ctx, "grpc ValidateLoginToken failed",
			"accountId", req.GetAccountId(),
			"error", err.Error(),
		)
		return nil, status.Error(codes.Internal, "validate login token failed")
	}

	return &loginproto.ValidateLoginTokenAck{Valid: valid}, nil
}
