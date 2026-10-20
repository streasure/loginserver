package service

import (
	"context"
	"github.com/streasure/loginserver/internal/config"

	"github.com/streasure/util/component"
	"github.com/streasure/util/netutil"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/ugrpc"
	"github.com/streasure/util/uload"
	"google.golang.org/grpc"

	loginproto "github.com/streasure/protocol/loginserver"
)

// LoginGrpcServer 负责 gRPC 服务器的生命周期管理（创建、启动、销毁），
// 业务逻辑由 LoginHandler 实现，通过组合方式注册到 gRPC 服务器。
type LoginGrpcServer struct {
	component.BaseComponent
	server *ugrpc.Server
}

func NewLoginGrpcServer() *LoginGrpcServer {
	cfg := config.GetConfig()
	s := &LoginGrpcServer{}

	// gRPC 服务端口为 0 时按配置禁用，不创建服务器
	if cfg.Ports.GrpcServiceAddr == 0 {
		tlog.Info(context.TODO(), "grpc service addr is empty, skip grpc server init")
		return s
	}

	// 创建通用 gRPC 服务器：身份字段（belong/serverType/zone/serverId）供 etcd
	// 服务注册使用（ServiceKey/ServerId）；只写端口时监听所有网卡，
	// 对外通告地址（etcd 注册用）通过 server.AdvertiseAddr() 自动拼接本机 IP。
	// 在构造函数创建（而非 Init），etcd 组件创建时即可读取服务身份。
	// 拦截器链：uload.UnaryServerInterceptor 最外（过载准入，与 HTTP 侧 uload.Middleware
	// 共享同一状态机；健康检查方法免准入）→ AdminAuthUnaryInterceptor（管理类 RPC 鉴权）。
	// 经 WithGrpcOptions 追加到 util 内置拦截器链之后
	s.server = ugrpc.NewServer(
		ugrpc.WithName("login-server"),
		ugrpc.WithAddr(netutil.PortAddr(cfg.Ports.GrpcServiceAddr)),
		ugrpc.WithBelong(cfg.Belong),
		ugrpc.WithServerType(cfg.ServerType),
		ugrpc.WithZone(cfg.Zone),
		ugrpc.WithServerId(cfg.ServerId),
		ugrpc.WithHealth(true),
		ugrpc.WithAPM(false), // 与 HTTP 侧一致，关闭 APM 全链路追踪
		ugrpc.WithGrpcOptions(grpc.ChainUnaryInterceptor(uload.UnaryServerInterceptor(), AdminAuthUnaryInterceptor)),
	)

	// 注册业务 handler
	loginproto.RegisterLoginServiceServer(s.server, NewLoginServer())
	s.server.SetServingStatus("loginserver.LoginService", true)

	return s
}

func (s *LoginGrpcServer) Start() error {
	if s.server == nil {
		return nil
	}

	return s.server.Start()
}

func (s *LoginGrpcServer) Destroy() {
	if s.server != nil {
		s.server.Stop()
	}
}
