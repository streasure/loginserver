package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/streasure/loginserver/internal/rmodel"

	protoGw "github.com/streasure/protocol/gateway"
	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/udiscovery"
	"github.com/streasure/util/uetcd"
	"github.com/streasure/util/ugrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// kickTimeout 单台 sgate 的 Kick RPC 超时（best-effort，不回滚封禁）
const kickTimeout = 2 * time.Second

// SgateClientPool 泛型 gRPC 客户端池：按 sgate 实例缓存 Kick 连接，
// 由 SGATE:{zone} 的 etcd Discovery 驱动（sgate 自注册 JSON 地址 →
// 经 udiscovery.WithAddressTransform 解析出 gRPC 拨号地址）。
type SgateClientPool struct {
	component.BaseComponent
	pool   *ugrpc.Pool[protoGw.KickServiceClient]
	source ugrpc.Source
}

var (
	sgatePool     *SgateClientPool
	sgatePoolInit atomic.Bool
)

// GetSgateClientPool 全局单例（main 在容器中挂载组件后可用）
func GetSgateClientPool() *SgateClientPool {
	return sgatePool
}

// sgateRegisterAddress 是 sgate etcd 注册地址的 JSON 结构（cluster_component.buildRegisterAddress）
type sgateRegisterAddress struct {
	IP   string `json:"ip"`
	GRPC int    `json:"grpc"`
}

// parseSgateGRPCAddr 从 sgate 注册地址解析 gRPC 拨号地址（udiscovery 的地址变换函数）：
// 非 JSON（未来改为纯地址注册）原样通过；JSON 但缺 ip/grpc 拨号要素或空地址视为不可用。
func parseSgateGRPCAddr(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	var addr sgateRegisterAddress
	if err := json.Unmarshal([]byte(raw), &addr); err != nil {
		return raw, true
	}
	if addr.IP != "" && addr.GRPC > 0 {
		return fmt.Sprintf("%s:%d", addr.IP, addr.GRPC), true
	}
	return "", false
}

// NewSgateClientPool 创建并注册全局单例；discovery 为 SGATE:{zone} 的 etcd 组件。
// 地址变换经 udiscovery.WithAddressTransform 在 Source 层统一作用于快照与事件。
func NewSgateClientPool(discovery *uetcd.Component, internalToken string) *SgateClientPool {
	p := &SgateClientPool{}
	if sgatePoolInit.CompareAndSwap(false, true) {
		sgatePool = p
	}
	p.pool = ugrpc.NewPool(ugrpc.PoolConfig[protoGw.KickServiceClient]{
		NewClient: func(cc *grpc.ClientConn) protoGw.KickServiceClient {
			return protoGw.NewKickServiceClient(cc)
		},
		DialOptions: append(
			ugrpc.TokenDialOptions("x-internal-token", internalToken),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	})
	if discovery != nil {
		p.source = udiscovery.NewEtcd(discovery, udiscovery.WithAddressTransform(parseSgateGRPCAddr))
		p.pool.Attach(p.source)
	}
	return p
}

// AllIDs 返回当前已发现的全部 sgate 实例 ID（CIDR 封禁无法用 sess:ip 索引路由，
// 需向全部 sgate 下发，各台本地前缀匹配）。
func (p *SgateClientPool) AllIDs() []string {
	if p.source == nil {
		return nil
	}
	set := p.source.ServiceSet()
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

func (p *SgateClientPool) Name() string { return "sgate-client-pool" }

func (p *SgateClientPool) Destroy() {
	if p.pool != nil {
		p.pool.Close()
	}
}

// KickIP 请求指定 sgate 踢掉该 IP 的全部本地连接；返回踢中数
func (p *SgateClientPool) KickIP(ctx context.Context, sgateID, ip, reason string) (int32, error) {
	client, ok := p.pool.Get(sgateID)
	if !ok {
		return 0, fmt.Errorf("sgate %s not in pool", sgateID)
	}
	ack, err := client.KickIP(ctx, &protoGw.KickIPRequest{Ip: ip, Reason: reason})
	if err != nil {
		return 0, err
	}
	return ack.GetKickedCount(), nil
}

// KickAccounts 请求指定 sgate 踢掉账号列表对应的本地连接；返回踢中数
func (p *SgateClientPool) KickAccounts(ctx context.Context, sgateID string, accountIDs []string, reason string) (int32, error) {
	client, ok := p.pool.Get(sgateID)
	if !ok {
		return 0, fmt.Errorf("sgate %s not in pool", sgateID)
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
	pool := GetSgateClientPool()
	if pool == nil {
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
			sgates = pool.AllIDs()
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
		// 与 CIDR 分支的 AllIDs 语义对齐，避免"封禁落库成功但一台不踢"
		if len(sgates) == 0 {
			sgates = pool.AllIDs()
		}
	}
	for _, sgateID := range sgates {
		cctx, cancel := context.WithTimeout(base, kickTimeout)
		n, err := pool.KickIP(cctx, sgateID, ip, "blacklist")
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
	pool := GetSgateClientPool()
	if pool == nil {
		return 0
	}
	base := context.WithoutCancel(ctx)
	sgates, err := rmodel.SessionSgatesByAccount(base, accountID)
	if err != nil {
		tlog.Warn(base, "kickAccountAfterBan session lookup failed account:%s err:%v", accountID, err)
	}
	for _, sgateID := range sgates {
		cctx, cancel := context.WithTimeout(base, kickTimeout)
		n, err := pool.KickAccounts(cctx, sgateID, []string{accountID}, "ban")
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
