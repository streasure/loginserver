package rpc

import (
	"context"
	"fmt"
	"loginserver/internal/config"
	"net"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"loginserver/internal/service"

	loginproto "github.com/streasure/protocol/loginserver"
)

type LoginGrpcServer struct {
	component.BaseComponent
	loginproto.UnimplementedLoginServiceServer
	server       *grpc.Server
	listener     net.Listener
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
	return nil
}

func (s *LoginGrpcServer) Start() error {
	port := s.config.Ports.GrpcServiceAddr
	if port == 0 {
		tlog.Info("grpc service addr is empty, skip grpc server start")
		return nil
	}
	addr := fmt.Sprintf(":%d", port)

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		tlog.Error("grpc listen failed", "addr", addr, "error", err.Error())
		return err
	}

	s.loginService = service.NewLoginService(config.GetConfig())

	s.server = grpc.NewServer(
		grpc.MaxConcurrentStreams(1000),
	)

	loginproto.RegisterLoginServiceServer(s.server, s)

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(s.server, healthServer)
	healthServer.SetServingStatus("loginserver.LoginService", healthpb.HealthCheckResponse_SERVING)

	tlog.Info("grpc server starting", "addr", addr)

	go func() {
		if err := s.server.Serve(s.listener); err != nil {
			tlog.Error("grpc server serve failed", "error", err.Error())
		}
	}()

	return nil
}

func (s *LoginGrpcServer) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *LoginGrpcServer) Destroy() {
	s.Stop()
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
