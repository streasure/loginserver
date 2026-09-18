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

func NewLoginGrpcServer(cfg *config.Config) *LoginGrpcServer {
	s := &LoginGrpcServer{
		config: cfg,
	}

	// gRPC 服务端口为 0 时按配置禁用，不创建服务器
	if cfg.Ports.GrpcServiceAddr == 0 {
		tlog.Info(context.Background(), "grpc service addr is empty, skip grpc server init")
		return s
	}

	// 创建通用 gRPC 服务器：身份字段（belong/serverType/zone/serverId）供 etcd
	// 服务注册使用（ServiceKey/ServerId）；只写端口时监听所有网卡，
	// 对外通告地址（etcd 注册用）通过 server.AdvertiseAddr() 自动拼接本机 IP。
	// 在构造函数创建（而非 Init），etcd 组件创建时即可读取服务身份
	s.server = ugrpc.NewServer(
		ugrpc.WithName("grpc-server"),
		ugrpc.WithAddr(fmt.Sprintf(":%d", cfg.Ports.GrpcServiceAddr)),
		ugrpc.WithBelong(cfg.Belong),
		ugrpc.WithServerType(cfg.ServerType),
		ugrpc.WithZone(cfg.Zone),
		ugrpc.WithServerId(cfg.ServerId),
		ugrpc.WithHealth(true),
		ugrpc.WithAPM(false),
	)

	// 注册业务 handler
	loginproto.RegisterLoginServiceServer(s.server, s)
	s.server.SetServingStatus("loginserver.LoginService", true)

	return s
}

// GrpcServer 返回内部通用 gRPC 服务器（gRPC 服务禁用时为 nil），
// 供 etcd 组件读取服务身份（ServiceKey/ServerId）与通告地址（AdvertiseAddr）
func (s *LoginGrpcServer) GrpcServer() *ugrpc.Server {
	return s.server
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
		tlog.Error(ctx, "grpc ValidateLoginToken failed",
			"accountId", req.GetAccountId(),
			"error", err.Error(),
		)
		return &loginproto.ValidateLoginTokenAck{Valid: false}, nil
	}

	return &loginproto.ValidateLoginTokenAck{Valid: valid}, nil
}
