package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/rmodel"

	"github.com/streasure/protocol/enums"
	protoGw "github.com/streasure/protocol/gateway"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/udiscovery"
)

// kickTimeout 单台 sgate 的 Kick RPC 超时（best-effort，不回滚封禁）
const kickTimeout = 2 * time.Second

// sgateServerType Kick 下发的目标服务类型（坐标第二段，sgate 自注册同型）
const sgateServerType = enums.ServerType_SERVER_TYPE_SGATE

// sgateKick 已绑定 SGATE 坐标的 Kick 客户端句柄（RegisterSgateClients 赋值，
// 未注册/注册失败时为 nil，取用安全返回 false）。实例增删由 etcd watch 自动驱动。
var sgateKick *udiscovery.Client[protoGw.KickServiceClient]

// RegisterSgateClients 注册 SGATE Kick 客户端句柄（main 装配期调用，
// 发现不可用即启动失败）。spec 用两个预设收敛拨号拼装：
//   - InternalToken：x-internal-token 鉴权 + insecure 凭证（与 sgate 侧 kickInternalToken 同值）；
//   - JSONAddr：sgate 自注册的 JSON 复合地址自动取 gRPC 拨号口。
//
// 客户端类型由首参 NewKickServiceClient 推导，调用面零泛型语法。
func RegisterSgateClients() error {
	cfg := config.GetConfig()
	h, err := udiscovery.Dial(protoGw.NewKickServiceClient, udiscovery.ClientSpec{
		Belong:        cfg.Belong,
		ServerType:    sgateServerType,
		Zone:          cfg.Zone,
		Etcd:          cfg.Uetcd(),
		InternalToken: cfg.Sgate.InternalToken,
		JSONAddr:      true,
	})
	if err != nil {
		return err
	}
	sgateKick = h
	return nil
}

// kickSgateIP 请求指定 sgate 踢掉该 IP 的全部本地连接；返回踢中数
func kickSgateIP(ctx context.Context, sgateID, ip, reason string) (int32, error) {
	client, ok := sgateKick.Get(sgateID)
	if !ok {
		return 0, fmt.Errorf("sgate %s not registered", sgateID)
	}
	ack, err := client.KickIP(ctx, &protoGw.KickIPRequest{Ip: ip, Reason: reason})
	if err != nil {
		return 0, err
	}
	return ack.GetKickedCount(), nil
}

// kickSgateAccounts 请求指定 sgate 踢掉账号列表对应的本地连接；返回踢中数
func kickSgateAccounts(ctx context.Context, sgateID string, accountIDs []string, reason string) (int32, error) {
	client, ok := sgateKick.Get(sgateID)
	if !ok {
		return 0, fmt.Errorf("sgate %s not registered", sgateID)
	}
	ack, err := client.KickAccounts(ctx, &protoGw.KickAccountsRequest{AccountIds: accountIDs, Reason: reason})
	if err != nil {
		return 0, err
	}
	return ack.GetKickedCount(), nil
}

// kickIPAfterBan 封禁 IP 后 best-effort 踢人。
// 纯 IP：用 sess:ip:{exact} 会话索引路由到持有该 IP 连接的 sgate，
//
//	索引缺失/查询失败时兜底全量下发（每台本地匹配）。
//
// CIDR：优先 sess:ip24 前缀索引精确路由（/24 及更小）；
//
//	/23 及更大网段或 IPv6 → 向全部已知 sgate 下发（每台本地前缀匹配）。
//
// 权威库已落库，失败仅告警计数、不回滚。
func kickIPAfterBan(ctx context.Context, ip string) (kicked int) {
	// 未注册/未发现任何 sgate（未配发现、或对端全离线）：跳过会话查询与下发。
	// 全量清单一次取定，同一笔封禁内各分支共用（避免分支间快照漂移）
	all := sgateKick.IDs()
	if len(all) == 0 {
		return 0
	}
	// 踢人链路不绑定调用方生命周期：admin/gRPC 请求取消或短 deadline
	// 不应掐断循环内后续 sgate 的 Kick（每台 2s 超时各自独立）
	base := context.WithoutCancel(ctx)
	var sgates []string
	if isCIDR(ip) {
		routed, needAll, err := rmodel.SessionSgatesByCIDR(base, ip)
		if err != nil {
			tlog.Warn(base, "kickAfterBan cidr route failed ip:%s err:%v", ip, err)
		}
		if needAll || len(routed) == 0 {
			sgates = all
		} else {
			sgates = routed
		}
	} else {
		var err error
		sgates, err = rmodel.SessionSgatesByIP(base, ip)
		if err != nil {
			tlog.Warn(base, "kickAfterBan session lookup failed ip:%s err:%v", ip, err)
		}
		// 索引缺失/查询失败时兜底全量下发（无该 IP 连接的 sgate 本地匹配为 0，安全），
		// 与 CIDR 分支的全量下发语义对齐，避免"封禁落库成功但一台不踢"
		if len(sgates) == 0 {
			sgates = all
		}
	}
	for _, sgateID := range sgates {
		cctx, cancel := context.WithTimeout(base, kickTimeout)
		n, err := kickSgateIP(cctx, sgateID, ip, "blacklist")
		cancel()
		if err != nil {
			tlog.Warn(base, "kickAfterBan KickIP failed sgate:%s ip:%s err:%v", sgateID, ip, err)
			continue
		}
		kicked += int(n)
	}
	if len(sgates) > 0 {
		tlog.Info(base, "kickAfterBan done ip:%s sgates:%d kicked:%d cidr:%v", ip, len(sgates), kicked, isCIDR(ip))
	}
	return kicked
}

// isCIDR 判断字符串是否为 CIDR 形态（含 '/'）
func isCIDR(s string) bool {
	return strings.Contains(s, "/")
}

// kickAccountAfterBan 封禁账号后 best-effort 踢人：会话缓存解析涉及的 sgate，
// 逐台 KickAccounts（每台 2s 超时）。权威库已落库，失败仅告警计数、不回滚。
// 与 kickIPAfterBan 一致：踢人链路不绑定调用方生命周期
func kickAccountAfterBan(ctx context.Context, accountID string) (kicked int) {
	// 未注册/未发现任何 sgate：跳过会话查询与下发
	if len(sgateKick.IDs()) == 0 {
		return 0
	}
	base := context.WithoutCancel(ctx)
	sgates, err := rmodel.SessionSgatesByAccount(base, accountID)
	if err != nil {
		tlog.Warn(base, "kickAccountAfterBan session lookup failed account:%s err:%v", accountID, err)
	}
	for _, sgateID := range sgates {
		cctx, cancel := context.WithTimeout(base, kickTimeout)
		n, err := kickSgateAccounts(cctx, sgateID, []string{accountID}, "ban")
		cancel()
		if err != nil {
			tlog.Warn(base, "kickAccountAfterBan KickAccounts failed sgate:%s account:%s err:%v", sgateID, accountID, err)
			continue
		}
		kicked += int(n)
	}
	if len(sgates) > 0 {
		tlog.Info(base, "kickAccountAfterBan done account:%s sgates:%d kicked:%d", accountID, len(sgates), kicked)
	}
	return kicked
}
