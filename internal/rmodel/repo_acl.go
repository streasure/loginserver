package rmodel

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// AclEntry ACL 缓存条目（仅用于 servers 列表）
type AclEntry struct {
	Data json.RawMessage `json:"data,omitempty"`
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
