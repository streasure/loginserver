package rpc

import (
	"context"
	"fmt"
	"loginserver/internal/config"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/ugrpc"

	loginproto "github.com/streasure/protocol/loginserver"

	"loginserver/internal/service"
)

type LoginGrpcServer struct {
	component.BaseComponent

	loginproto.UnimplementedLoginServiceServer

	server       *ugrpc.Server
	config       *config.Config
	loginService *service.LoginService
}

func NewLoginGrpcServer(config *config.Config) *LoginGrpcServer {
	return &LoginGrpcServer{
		config: config,
	}
}

func (s *LoginGrpcServer) Name() string {
	return "grpc-server"
}

func (s *LoginGrpcServer) Init() error {
	port := s.config.Ports.GrpcServiceAddr
	if port == 0 {
		tlog.Info("grpc service addr is empty, skip grpc server init")
		return nil
	}

	// 创建通用 gRPC 服务器，只写端口时监听所有网卡，
	// 对外通告地址（etcd 注册用）通过 server.AdvertiseAddr() 自动拼接本机 IP
	s.server = ugrpc.NewServer(
		ugrpc.WithName("grpc-server"),
		ugrpc.WithAddr(fmt.Sprintf(":%d", port)),
		ugrpc.WithHealth(true),
	)

	// 注册业务 handler
	loginproto.RegisterLoginServiceServer(s.server, s)
	s.server.SetServingStatus("loginserver.LoginService", true)

	return nil
}

func (s *LoginGrpcServer) Start() error {
	if s.server == nil {
		return nil
	}

	s.loginService = service.NewLoginService(config.GetConfig())

	return s.server.Start()
}

func (s *LoginGrpcServer) Destroy() {
	if s.server != nil {
		s.server.Stop()
	}
}

func (s *LoginGrpcServer) ValidateLoginToken(ctx context.Context, req *loginproto.ValidateLoginTokenReq) (*loginproto.ValidateLoginTokenAck, error) {
	valid, err := s.loginService.ValidateLoginToken(ctx, req.GetAccountId(), req.GetLoginToken())
	if err != nil {
		tlog.ErrorContext(ctx, "grpc ValidateLoginToken failed",
			"accountId", req.GetAccountId(),
			"error", err.Error(),
		)
		return &loginproto.ValidateLoginTokenAck{Valid: false}, nil
	}

	return &loginproto.ValidateLoginTokenAck{Valid: valid}, nil
}
