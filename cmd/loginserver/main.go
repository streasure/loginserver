package main

import (
	"flag"
	"fmt"
	"loginserver/internal"
	internalcomponent "loginserver/internal/component"
	"loginserver/internal/config"
	_ "loginserver/internal/handler"
	"loginserver/internal/rpc"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
	"github.com/streasure/util/upprof"
	"github.com/streasure/util/uperf"
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
		tlog.Error("load config failed error", err)
		return
	}

	conf := config.GetConfig()

	// 应用运行时性能参数（GC 调优、内存软上限），须在任何组件分配大量内存前执行
	uperf.Apply(conf.Perf.GcPercent, conf.Perf.MemoryLimitPercent)

	// 创建容器
	container := component.NewContainer()

	// 初始化基础组件 (Redis, Etcd)
	container.Add(internalcomponent.NewRedisComponent())
	container.Add(internalcomponent.NewEtcdComponent())

	// gRPC 业务组件
	container.Add(rpc.NewLoginGrpcServer(conf))

	// HTTP 业务组件
	container.Add(ugin.NewComponent(conf.Belong, fmt.Sprintf(":%d", conf.Ports.HttpAddr)))

	// pprof
	container.Add(upprof.NewUPprofComponent(fmt.Sprintf(":%d", conf.Ports.PprofPort)))

	tlog.Info("loginserver starting",
		"belong", conf.Belong,
		"serverType", conf.ServerType,
		"zone", conf.Zone,
		"serverId", conf.ServerId,
		"httpAddr", conf.Ports.HttpAddr,
		"grpcAddr", conf.Ports.GrpcServiceAddr,
	)

	// 启动所有组件
	container.Serve()

	tlog.Info("loginserver stopped")
}
