package rmodel

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// 会话缓存（踢人路由索引，单向 Kick 模型 v2）：
//
//	sess:conn:{sgateId}:{connId} hash  {account_id, ip, login_at}
//	sess:acct:{accountId}         set  成员 {sgateId}:{connId}
//	sess:ip:{ip}                  set  成员 {sgateId}:{connId}
//
// 由 loginserver 在 ValidateLoginToken 校验通过（sgate 携带会话上下文）时写入；
// 无 Unbind，靠 TTL 收敛；封禁时 SMEMBERS 解析 sgateId 去重后下发 Kick。
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

// sessionMember 三键共用的集合成员：{sgateId}:{connId}
func sessionMember(sgateID, connID string) string {
	return sgateID + ":" + connID
}

// WriteSession 写入三键会话缓存（pipeline 原子提交，TTL 24h）。
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
