package main

import (
	"context"
	"flag"
	"fmt"
	"loginserver/internal"
	internalcomponent "loginserver/internal/component"
	"loginserver/internal/config"
	"loginserver/internal/handler"
	"loginserver/internal/rmodel"
	"loginserver/internal/rpc"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
	"github.com/streasure/util/uperf"
	"github.com/streasure/util/upprof"
)

var (
	confFiles  = flag.String("conf", "", "specify config file")
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
		fmt.Printf("failed to initialize tlog: %v\n", err)
		return
	}
	defer logComp.Destroy()

	// 加载配置
	err := config.LoadConfig(*confFiles)
	if err != nil {
		tlog.Error(context.Background(), "load config failed error", "error", err)
		return
	}

	conf := config.GetConfig()

	// 初始化 rmodel Redis key 前缀
	rmodel.Init(conf.Belong, conf.ServerType, conf.Zone)

	// 注册 HTTP 路由
	handler.RegisterRoutes()

	// 应用运行时性能参数（GC 调优、内存软上限），须在任何组件分配大量内存前执行
	uperf.Apply(conf.Perf.GcPercent, conf.Perf.MemoryLimitPercent)

	// 创建容器
	container := component.NewContainer()

	// 组件启动顺序：etcd（注册中心）由 util 默认排在最后启动（Order 最大，销毁时最先注销），
	// 其余组件按 Add 顺序——redis 需先于 rpc/ugin（业务组件初始化依赖 redis 客户端）

	// redis
	container.Add(internalcomponent.NewRedisComponent())

	// gRPC 业务组件
	rpcServer := rpc.NewLoginGrpcServer()
	container.Add(rpcServer)

	// etcd（服务身份与通告地址取自 rpcServer 内的 gRPC 服务器，取不到直接报错退出）
	etcdComp, err := internalcomponent.NewEtcdComponent(rpcServer)
	if err != nil {
		tlog.Error(context.Background(), "create etcd component failed", "error", err.Error())
		return
	}
	container.Add(etcdComp)

	// HTTP 业务组件
	container.Add(ugin.NewComponent(conf.Belong, fmt.Sprintf(":%d", conf.Ports.HttpAddr),
		ugin.WithAPM(false),
	))

	// pprof
	container.Add(upprof.NewUPprofComponent(fmt.Sprintf(":%d", conf.Ports.PprofPort)))

	tlog.Info(context.Background(), "loginserver starting",
		"belong", conf.Belong,
		"serverType", conf.ServerType,
		"zone", conf.Zone,
		"serverId", conf.ServerId,
		"httpAddr", conf.Ports.HttpAddr,
		"grpcAddr", conf.Ports.GrpcServiceAddr,
	)

	// 启动所有组件
	container.Serve()

	tlog.Info(context.Background(), "loginserver stopped")
}
