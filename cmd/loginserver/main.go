package main

import (
	"flag"
	"fmt"
	"os"

	"loginserver/internal"
	_ "loginserver/internal/handler"
	"loginserver/internal/rpc"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
)

var (
	confs      = flag.String("conf", "", "specify config file")
	loggerConf = flag.String("logger", "", "logger config file")
	showVer    = flag.Bool("version", false, "show version")
)

func main() {
	flag.Parse()
	if *showVer {
		fmt.Printf("loginserver version: %s\n", internal.Version)
		return
	}

	// 初始化 tlog
	logComp := tlog.NewLogComponent(*loggerConf)
	if err := logComp.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize tlog: %v\n", err)
		return
	}
	defer logComp.Destroy()

	// 加载配置
	if err := internal.Load(*confs); err != nil {
		tlog.Error("load config failed", "error", err.Error())
		os.Exit(1)
	}

	config := internal.GetConfig()
	internal.InitOptions(
		internal.WithBelong(config.Belong),
		internal.WithServerType(config.ServerType),
		internal.WithZone(config.Zone),
		internal.WithServerId(config.ServerId),
		internal.WithLoginTokenExpireSeconds(config.Limits.LoginTokenExpireSeconds),
		internal.WithClientGetServerListUrl(config.ServerList.ClientGetServerListUrl),
		internal.WithServerInfos(config.ServerList.ServerInfos),
		internal.WithHttpAddr(config.Ports.HttpAddr),
		internal.WithGrpcServiceAddr(config.Ports.GrpcServiceAddr),
	)

	// 创建容器
	container := component.NewContainer()

	// 初始化基础组件 (Redis, Etcd)
	internal.InitBaseComponent(container, config)

	// gRPC 业务组件
	grpcServer := rpc.NewLoginGrpcServer(config)
	container.Add(grpcServer)

	// HTTP 业务组件
	ginManager := ugin.NewComponent("loginserver", config.Ports.HttpAddr)
	container.Add(ginManager)

	tlog.Info("loginserver starting",
		"belong", config.Belong,
		"serverType", config.ServerType,
		"zone", config.Zone,
		"serverId", config.ServerId,
		"httpAddr", config.Ports.HttpAddr,
		"grpcAddr", config.Ports.GrpcServiceAddr,
	)

	// 启动所有组件
	container.Serve()

	tlog.Info("loginserver stopped")
}
