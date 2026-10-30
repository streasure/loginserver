package rmodel

import (
	"context"
	"encoding/json"
	"time"

	"github.com/streasure/loginserver/internal/dmodel"
)

// 服务器列表 Redis 缓存（单键）：MySQL 为权威源，缓存仅省回源往返。
// 脏数据（JSON 损坏）按缺失处理，下次回源覆盖。

// serversCacheKey 服务器列表缓存键：{belong}/{serverType}:{zone}:servers
func serversCacheKey() string {
	return getKey("servers")
}

// GetServersCache 读取缓存的服务器列表；未命中/脏数据返回 ok=false
func GetServersCache(ctx context.Context) ([]dmodel.ServerInfo, bool) {
	cli, err := GetRedisClient()
	if err != nil {
		return nil, false
	}
	raw, err := cli.Get(ctx, serversCacheKey()).Bytes()
	if err != nil {
		return nil, false
	}
	var list []dmodel.ServerInfo
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	return list, true
}

// SetServersCache 写入服务器列表缓存（带 TTL，写失败仅调用方告警）
func SetServersCache(ctx context.Context, list []dmodel.ServerInfo, ttl time.Duration) error {
	cli, err := GetRedisClient()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return cli.Set(ctx, serversCacheKey(), raw, ttl).Err()
}
