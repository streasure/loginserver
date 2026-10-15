package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/streasure/loginserver/internal/rmodel"

	protoGw "github.com/streasure/protocol/gateway"
	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/uetcd"
	"github.com/streasure/util/ugrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// kickTimeout 单台 sgate 的 Kick RPC 超时（best-effort，不回滚封禁）
const kickTimeout = 2 * time.Second

// SgateClientPool 泛型 gRPC 客户端池：按 sgate 实例缓存 Kick 连接，
// 由 SGATE:{zone} 的 etcd Discovery 驱动（sgate 自注册 JSON 地址 → 解析出 gRPC 拨号地址）。
type SgateClientPool struct {
	component.BaseComponent
	pool *ugrpc.Pool[protoGw.KickServiceClient]
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

// parseSgateGRPCAddr 从 sgate 注册地址解析 gRPC 拨号地址 "ip:port"；
// 非 JSON（未来改为纯地址注册）时原样返回
func parseSgateGRPCAddr(raw string) string {
	var addr sgateRegisterAddress
	if err := json.Unmarshal([]byte(raw), &addr); err == nil && addr.IP != "" && addr.GRPC > 0 {
		return fmt.Sprintf("%s:%d", addr.IP, addr.GRPC)
	}
	return raw
}

// sgateSource 将 uetcd.Component 适配为 ugrpc.Source（消费方本地适配器，util 解耦 V5 约定）
type sgateSource struct {
	c *uetcd.Component
}

func (s sgateSource) ServiceSet() map[string]string {
	in := s.c.ServiceSet()
	out := make(map[string]string, len(in))
	for instanceID, addr := range in {
		if grpcAddr := parseSgateGRPCAddr(addr); grpcAddr != "" {
			out[instanceID] = grpcAddr
		}
	}
	return out
}

func (s sgateSource) OnServiceChange(cb func(event ugrpc.SourceEvent)) {
	s.c.OnServiceChange(func(ev uetcd.ServiceEvent) {
		addr := ev.Address
		if ev.Type == uetcd.EventRegister {
			addr = parseSgateGRPCAddr(addr)
		}
		cb(ugrpc.SourceEvent{
			Register:   ev.Type == uetcd.EventRegister,
			InstanceID: ev.InstanceID,
			Address:    addr,
		})
	})
}

// NewSgateClientPool 创建并注册全局单例；discovery 为 SGATE:{zone} 的 etcd 组件
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
		p.pool.Attach(sgateSource{c: discovery})
	}
	return p
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
func (p *SgateClientPool) KickAccounts(ctx context.Context, sgateID string, accountIDs []int64, reason string) (int32, error) {
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

// kickIPAfterBan 封禁 IP 后 best-effort 踢人：会话缓存解析涉及的 sgate，
// 逐台 KickIP（每台 2s 超时）。权威库已落库，失败仅告警计数、不回滚
func kickIPAfterBan(ctx context.Context, ip string) (kicked int) {
	pool := GetSgateClientPool()
	if pool == nil {
		return 0
	}
	sgates, err := rmodel.SessionSgatesByIP(ctx, ip)
	if err != nil {
		tlog.Warn(ctx, "kickAfterBan session lookup failed ip:%s err:%v", ip, err)
	}
	for _, sgateID := range sgates {
		cctx, cancel := context.WithTimeout(ctx, kickTimeout)
		n, err := pool.KickIP(cctx, sgateID, ip, "blacklist")
		cancel()
		if err != nil {
			tlog.Warn(ctx, "kickAfterBan KickIP failed sgate:%s ip:%s err:%v", sgateID, ip, err)
			continue
		}
		kicked += int(n)
	}
	if len(sgates) > 0 {
		tlog.Info(ctx, "kickAfterBan done ip:%s sgates:%d kicked:%d", ip, len(sgates), kicked)
	}
	return kicked
}
