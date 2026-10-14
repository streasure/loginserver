package rmodel

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// AclEntry ACL 缓存条目。ver 为写入时的全局版本号（acl:gen 计数），
// hit 为判定结果（含负缓存：不在名单也缓存，避免反复回源）。
// 读侧本地版本快照低于 ver 时条目视为过期并回源。
type AclEntry struct {
	Ver  int64           `json:"ver"`
	Hit  bool            `json:"hit"`
	Data json.RawMessage `json:"data,omitempty"` // servers 列表等大条目负载
}

// ACL 缓存键段（kind）：白名单/黑名单/封禁角色/服务器列表
const (
	AclKindWhiteIP = "wl"
	AclKindBlackIP = "bl"
	AclKindRole    = "role"
	AclKindServers = "servers"
)

func aclKey(kind, id string) string {
	if id == "" {
		return getKey("acl", kind)
	}
	return getKey("acl", kind, id)
}

func aclGenKey() string { return getKey("acl", "gen") }

// GetAclGen 读取全局版本号，键不存在视为 0
func GetAclGen(ctx context.Context) (int64, error) {
	client, err := GetRedisClient()
	if err != nil {
		return 0, err
	}
	v, err := client.Get(ctx, aclGenKey()).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

// IncrAclGen 版本号自增（每次 ACL 写库后调用），返回新版本号
func IncrAclGen(ctx context.Context) (int64, error) {
	client, err := GetRedisClient()
	if err != nil {
		return 0, err
	}
	return client.Incr(ctx, aclGenKey()).Result()
}

// GetAclEntry 读取缓存条目；键不存在返回 (nil, nil)
func GetAclEntry(ctx context.Context, kind, id string) (*AclEntry, error) {
	client, err := GetRedisClient()
	if err != nil {
		return nil, err
	}
	raw, err := client.Get(ctx, aclKey(kind, id)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry := &AclEntry{}
	if err := json.Unmarshal([]byte(raw), entry); err != nil {
		// 脏数据按缺失处理，回源后覆盖
		return nil, nil
	}
	return entry, nil
}

// SetAclEntry 写入缓存条目（带 TTL）
func SetAclEntry(ctx context.Context, kind, id string, entry *AclEntry, ttl time.Duration) error {
	client, err := GetRedisClient()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return client.Set(ctx, aclKey(kind, id), raw, ttl).Err()
}
