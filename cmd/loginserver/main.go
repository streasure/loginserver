package main

import (
	"context"
	"flag"
	"fmt"

	internalcomponent "github.com/streasure/loginserver/internal/component"

	"github.com/streasure/loginserver/internal"
	"github.com/streasure/loginserver/internal/config"
	_ "github.com/streasure/loginserver/internal/httphandler"
	"github.com/streasure/loginserver/internal/service"

	"github.com/streasure/util/component"
	"github.com/streasure/util/netutil"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/ugin"
	"github.com/streasure/util/ugorm"
	"github.com/streasure/util/uload"
	"github.com/streasure/util/uperf"
	"github.com/streasure/util/upprof"
	"github.com/streasure/util/uredis"
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

	// 创建容器
	container := component.NewContainer()

	// 运行时性能参数（GC 调优、内存软上限）：Order 最先，Init 时先于任何业务组件
	// 分配大量内存前应用，Destroy 时恢复应用前的运行时参数
	container.Add(uperf.New(uperf.Config{
		GcPercent:          conf.Perf.GcPercent,
		MemoryLimitPercent: conf.Perf.MemoryLimitPercent,
	}))

	// 组件启动顺序：uperf Order 最先；etcd（注册中心）由 util 默认排在最后启动
	// （Order 最大，销毁时最先注销），其余组件按 Add 顺序——redis 需先于 rpc/ugin
	// （业务组件初始化依赖 redis 客户端）

	// redis：配置解析/校验在 uredis.Config，此处一行创建并注册全局实例
	// （rmodel 经 uredis.GetClient("rdb") 取用）
	container.Add(uredis.Init(conf.Redis))

	// gorm：业务库（白/黑名单、封禁角色、服务器列表），配置形态对齐 ugorm.Config，
	// 须在业务组件（rpc/ugin）之前 Init，dmodel 经 ugorm.GetDbContext 取用
	container.Add(ugorm.NewComponent(&conf.Db))

	// ACL 服务：启动全量加载黑白名单/封禁角色快照（失败即失败启动），后台 gen 轮询同步其他节点写入；
	// 须在 gorm 之后 Init（快照加载依赖 MySQL）
	container.Add(service.GetAclService())

	// token manager（loginToken 校验的进程内缓存，须在 rpc 之前 Init）
	container.Add(service.GetTokenManager())

	// gRPC 业务组件
	rpcServer := service.NewLoginGrpcServer()
	container.Add(rpcServer)

	// etcd
	etcdComp, err := internalcomponent.NewEtcdComponent()
	if err != nil {
		tlog.Error(context.TODO(), "create etcd component failed:%v", err)
		return
	}
	container.Add(etcdComp)

	// SGATE 发现 + Kick 客户端池（单向 Kick 模型：封禁落库后经池内连接下发 KickIP/KickAccounts）
	sgateDisc, err := internalcomponent.NewSgateDiscoveryComponent()
	if err != nil {
		tlog.Error(context.TODO(), "create sgate discovery failed:%v", err)
		return
	}
	container.Add(sgateDisc)
	container.Add(service.NewSgateClientPool(sgateDisc.Inner(), conf.Sgate.InternalToken))

	// HTTP 业务组件；uload 自适应准入由 LoadComponent 随容器接入（边界全为 0 时惰性
	// 不生效），gin 中间件经 WithPreMiddleware 显式挂载（util 解耦后不再隐式接线），
	// 与 gRPC 拦截器共享同一状态机；
	// TrustedProxies 防 X-Forwarded-For 伪造（默认只信 TCP 对端，反代后需配置）
	container.Add(internalcomponent.NewLoadComponent(conf.Load))
	container.Add(ugin.NewComponent(conf.ServiceKey, netutil.PortAddr(conf.Ports.HttpAddr),
		ugin.WithAPM(false),
		ugin.WithPreMiddleware(uload.Middleware()),
		ugin.WithTrustedProxies(conf.Acl.TrustedProxies),
	))

	// pprof
	container.Add(upprof.NewUPprofComponent(netutil.PortAddr(conf.Ports.PprofPort)))

	tlog.Info(context.TODO(), "loginserver starting belong:%s serverType:%s zone:%s serverId:%s httpAddr:%d grpcAddr:%d",
		conf.Belong, conf.ServerType, conf.Zone, conf.ServerId, conf.Ports.HttpAddr, conf.Ports.GrpcServiceAddr)

	// 启动所有组件
	if err := container.Serve(); err != nil {
		tlog.Error(context.TODO(), "loginserver serve failed:%v", err)
	}

	tlog.Info(context.TODO(), "loginserver stopped")
}
