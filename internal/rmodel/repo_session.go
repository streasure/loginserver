package rmodel

import (
	"context"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// 会话缓存（踢人路由索引，单向 Kick 模型 v2）：
//
//	sess:conn:{sgateId}:{connId} hash  {account_id, ip, login_at}
//	sess:acct:{accountId}         set  成员 {sgateId}:{connId}
//	sess:ip:{ip}                  set  成员 {sgateId}:{connId}
//	sess:ip24:{a.b.c.0/24}        set  成员 {sgateId}:{connId}（CIDR 踢人路由用）
//
// 由 loginserver 在 ValidateLoginToken 校验通过（sgate 携带会话上下文）时写入；
// 无 Unbind 靠 TTL 收敛，但 sgate OnClose 会 best-effort 调 RemoveSession 清理。
// 封禁时 SMEMBERS 解析 sgateId 去重后下发 Kick；CIDR 封禁走 ip24 前缀键路由。
const sessionCacheTTL = 24 * time.Hour

func sessionConnKey(sgateID, connID string) string {
	return getKey("sess", "conn", sgateID, connID)
}

func sessionAcctKey(accountID string) string {
	return getKey("sess", "acct", accountID)
}

func sessionIPKey(ip string) string {
	return getKey("sess", "ip", ip)
}

// sessionIP24Key IPv4 /24 前缀键（CIDR 踢人路由）；IPv6 无 ip24 索引（前缀太大）。
func sessionIP24Key(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return ""
	}
	prefix := netip.PrefixFrom(addr, 24)
	return getKey("sess", "ip24", prefix.String())
}

// sessionMember 三键共用的集合成员：{sgateId}:{connId}
func sessionMember(sgateID, connID string) string {
	return sgateID + ":" + connID
}

// WriteSession 写入会话缓存（pipeline 原子提交，TTL 24h）。
// IPv4 额外写 sess:ip24:{/24} 前缀键（CIDR 踢人路由）。
// 失败不影响登录主流程（调用方 best-effort 记日志）。
func WriteSession(ctx context.Context, sgateID, connID, accountID, ip string) error {
	if sgateID == "" || connID == "" || accountID == "" {
		return nil
	}
	rdb, err := GetRedisClient()
	if err != nil {
		return err
	}

	member := sessionMember(sgateID, connID)
	connKey := sessionConnKey(sgateID, connID)
	acctKey := sessionAcctKey(accountID)
	ipKey := sessionIPKey(ip)
	ip24Key := sessionIP24Key(ip)

	pipe := rdb.TxPipeline()
	pipe.HSet(ctx, connKey,
		"account_id", accountID,
		"ip", ip,
		"login_at", strconv.FormatInt(time.Now().Unix(), 10),
	)
	pipe.Expire(ctx, connKey, sessionCacheTTL)
	if ip != "" {
		pipe.SAdd(ctx, ipKey, member)
		pipe.Expire(ctx, ipKey, sessionCacheTTL)
	}
	if ip24Key != "" {
		pipe.SAdd(ctx, ip24Key, member)
		pipe.Expire(ctx, ip24Key, sessionCacheTTL)
	}
	pipe.SAdd(ctx, acctKey, member)
	pipe.Expire(ctx, acctKey, sessionCacheTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// sessionSgates 从集合成员解析去重后的 sgateId 列表（成员格式 {sgateId}:{connId}）。
func sessionSgates(ctx context.Context, key string) ([]string, error) {
	rdb, err := GetRedisClient()
	if err != nil {
		return nil, err
	}
	members, err := rdb.SMembers(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(members))
	out := make([]string, 0, len(members))
	for _, m := range members {
		sgateID, _, ok := strings.Cut(m, ":")
		if !ok || sgateID == "" {
			continue
		}
		if _, dup := seen[sgateID]; dup {
			continue
		}
		seen[sgateID] = struct{}{}
		out = append(out, sgateID)
	}
	return out, nil
}

// SessionSgatesByIP 查询持有该 IP 会话的 sgate 实例（踢人路由）。
func SessionSgatesByIP(ctx context.Context, ip string) ([]string, error) {
	if ip == "" {
		return nil, nil
	}
	return sessionSgates(ctx, sessionIPKey(ip))
}

// SessionSgatesByAccount 查询持有该账号会话的 sgate 实例（批量踢账号预留）。
func SessionSgatesByAccount(ctx context.Context, accountID string) ([]string, error) {
	if accountID == "" {
		return nil, nil
	}
	return sessionSgates(ctx, sessionAcctKey(accountID))
}

// SessionSgatesByCIDR 查询 CIDR 网段覆盖的 sgate 实例（CIDR 踢人路由）。
// 策略：对 /24 及更小的 CIDR，展开覆盖的 /24 前缀键并集去重；
// /23 及更大的网段（覆盖面大）直接返回全部已知 sgate（调用方 fallback）。
// 返回 (sgates, needAll)：needAll=true 表示网段太大无法精确路由。
func SessionSgatesByCIDR(ctx context.Context, cidr string) (sgates []string, needAll bool, err error) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, false, err
	}
	// /23 及更大（前缀位数更小）→ 覆盖面太大，返回 needAll
	if p.Bits() < 24 && p.Addr().Is4() {
		return nil, true, nil
	}
	// IPv6 无 ip24 索引
	if p.Addr().Is6() {
		return nil, true, nil
	}
	// 展开覆盖的 /24 前缀（最多 2^（24-bits) 个；/24=1, /25=2, ... /32=256）
	seen := make(map[string]struct{})
	for a := p.Masked().Addr(); p.Contains(a); {
		key := sessionIP24Key(a.String())
		if key != "" {
			members, smErr := sessionSgates(ctx, key)
			if smErr != nil {
				return nil, false, smErr
			}
			for _, m := range members {
				seen[m] = struct{}{}
			}
		}
		// 推进到下一个 /24
		a = a.Next()
		if !a.IsValid() || a.Is4() && a.As4() == [4]byte{255, 255, 255, 255} {
			break
		}
		// 检查是否已超出前缀范围
		if !p.Contains(a) {
			break
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out, false, nil
}

// RemoveSession 连接关闭时清理会话缓存（best-effort，sgate OnClose 调用）。
// 先读 conn hash 拿 account_id/ip，再 pipeline 删 conn + SREM acct/ip/ip24 成员。
// 幂等：键不存在时 no-op。失败仅返回 error，调用方记日志不重试（靠 TTL 兜底）。
func RemoveSession(ctx context.Context, sgateID, connID string) error {
	if sgateID == "" || connID == "" {
		return nil
	}
	rdb, err := GetRedisClient()
	if err != nil {
		return err
	}
	connKey := sessionConnKey(sgateID, connID)
	member := sessionMember(sgateID, connID)

	// 先读 hash 拿 account/ip（SREM 需要精确键）
	vals, err := rdb.HMGet(ctx, connKey, "account_id", "ip").Result()
	if err != nil {
		return err
	}
	accountID, _ := vals[0].(string)
	ip, _ := vals[1].(string)

	pipe := rdb.TxPipeline()
	pipe.Del(ctx, connKey)
	if accountID != "" {
		pipe.SRem(ctx, sessionAcctKey(accountID), member)
	}
	if ip != "" {
		pipe.SRem(ctx, sessionIPKey(ip), member)
		if ip24Key := sessionIP24Key(ip); ip24Key != "" {
			pipe.SRem(ctx, ip24Key, member)
		}
	}
	_, err = pipe.Exec(ctx)
	return err
}

// SessionSummary 会话摘要（admin 列表用，不含敏感字段）
type SessionSummary struct {
	SgateID   string `json:"sgateId"`
	ConnID    string `json:"connId"`
	AccountID string `json:"accountId"`
	IP        string `json:"ip"`
	LoginAt   string `json:"loginAt"`
}

// ListSessions SCAN 扫码列出会话（admin 运维用）。
// pattern：sess:conn:* 前缀；sgateFilter 非空时只返回该 sgate 的会话。
// limit 控制最大返回条数。仅读，不改任何键。
func ListSessions(ctx context.Context, limit int, sgateFilter string) ([]SessionSummary, error) {
	rdb, err := GetRedisClient()
	if err != nil {
		return nil, err
	}
	prefix := getKey("sess", "conn") + ":"
	pattern := prefix + "*"
	out := make([]SessionSummary, 0, 64)
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			rest := key[len(prefix):]
			sgateID, connID, ok := strings.Cut(rest, ":")
			if !ok {
				continue
			}
			if sgateFilter != "" && sgateID != sgateFilter {
				continue
			}
			vals, err := rdb.HMGet(ctx, key, "account_id", "ip", "login_at").Result()
			if err != nil {
				continue // 单键失败跳过（可能刚好过期）
			}
			accountID, _ := vals[0].(string)
			ip, _ := vals[1].(string)
			loginAt, _ := vals[2].(string)
			out = append(out, SessionSummary{
				SgateID:   sgateID,
				ConnID:    connID,
				AccountID: accountID,
				IP:        ip,
				LoginAt:   loginAt,
			})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}
