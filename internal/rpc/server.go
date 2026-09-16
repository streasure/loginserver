package rpc

import (
	"context"
	"net"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	loginproto "github.com/streasure/protocol/loginserver"
	"loginserver/internal"
	"loginserver/internal/service"
)

type LoginGrpcServer struct {
	component.BaseComponent
	loginproto.UnimplementedLoginServiceServer
	server       *grpc.Server
	listener     net.Listener
	config       *internal.Config
	loginService *service.LoginService
}

func NewLoginGrpcServer(config *internal.Config) *LoginGrpcServer {
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
	addr := s.config.Ports.GrpcServiceAddr
	if addr == "" {
		tlog.Info("grpc service addr is empty, skip grpc server start")
		return nil
	}

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		tlog.Error("grpc listen failed", "addr", addr, "error", err.Error())
		return err
	}

	options := internal.InitOptions(
		internal.WithBelong(s.config.Belong),
		internal.WithServerType(s.config.ServerType),
		internal.WithZone(s.config.Zone),
		internal.WithServerId(s.config.ServerId),
		internal.WithLoginTokenExpireSeconds(s.config.Limits.LoginTokenExpireSeconds),
		internal.WithClientGetServerListUrl(s.config.ServerList.ClientGetServerListUrl),
		internal.WithServerInfos(s.config.ServerList.ServerInfos),
	)
	s.loginService = service.NewLoginService(options)

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
