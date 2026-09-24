package main

import (
	"context"
	"flag"
	"fmt"
	"loginserver/internal"
	internalcomponent "loginserver/internal/component"
	"loginserver/internal/config"
	"loginserver/internal/grpchandler"
	_ "loginserver/internal/httphandler"
	"loginserver/internal/service"

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
		tlog.Error(context.TODO(), "load config failed error:%v", err)
		return
	}

	conf := config.GetConfig()

	// 应用运行时性能参数（GC 调优、内存软上限），须在任何组件分配大量内存前执行
	uperf.Apply(conf.Perf.GcPercent, conf.Perf.MemoryLimitPercent)

	// 创建容器
	container := component.NewContainer()

	// 组件启动顺序：etcd（注册中心）由 util 默认排在最后启动（Order 最大，销毁时最先注销），
	// 其余组件按 Add 顺序——redis 需先于 rpc/ugin（业务组件初始化依赖 redis 客户端）

	// redis
	container.Add(internalcomponent.NewRedisComponent())

	// token manager（loginToken 校验的进程内缓存，须在 rpc 之前 Init）
	container.Add(service.GetTokenManager())

	// gRPC 业务组件
	rpcServer := grpchandler.NewLoginGrpcServer()
	container.Add(rpcServer)

	// etcd
	etcdComp, err := internalcomponent.NewEtcdComponent()
	if err != nil {
		tlog.Error(context.TODO(), "create etcd component failed:%v", err)
		return
	}
	container.Add(etcdComp)

	// HTTP 业务组件
	container.Add(ugin.NewComponent(conf.ServiceKey, fmt.Sprintf(":%d", conf.Ports.HttpAddr),
		ugin.WithAPM(false),
	))

	// pprof
	container.Add(upprof.NewUPprofComponent(fmt.Sprintf(":%d", conf.Ports.PprofPort)))

	tlog.Info(context.TODO(), "loginserver starting belong:%s serverType:%s zone:%s serverId:%s httpAddr:%d grpcAddr:%d",
		conf.Belong, conf.ServerType, conf.Zone, conf.ServerId, conf.Ports.HttpAddr, conf.Ports.GrpcServiceAddr)

	// 启动所有组件
	container.Serve()

	tlog.Info(context.TODO(), "loginserver stopped")
}
