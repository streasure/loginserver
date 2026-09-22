// Package rmodel Redis 数据访问层，封装所有 Redis key 构建和命令执行。
// 业务层直接调用包级函数完成 Redis 操作，不直接接触 Redis 客户端。
package rmodel

import (
	"errors"
	"loginserver/internal/config"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/uredis"
)

// ErrRedisUnavailable Redis 客户端未就绪
var ErrRedisUnavailable = errors.New("redis client unavailable")

// GetRedisCli 返回全局 Redis 客户端（从 component 容器获取）
func GetRedisCli() (redis.Cmdable, error) {
	if comp := uredis.GetClient("rdb").GetRawGoRedisClient(); comp != nil {
		return comp, nil
	}
	return nil, ErrRedisUnavailable
}

// accountKey 拼接 account 绑定的 Redis key：
// {belong}/{serverType}:{zone}:account:{openId}:{ptId}
func accountKey(openId string, ptId int32) string {
	return getKey("account", openId, strconv.Itoa(int(ptId)))
}

// loginTokenKey 拼接 loginToken 的 Redis key：
// {belong}/{serverType}:{zone}:loginToken:{accountId}
func loginTokenKey(accountId string) string {
	return getKey("loginToken", accountId)
}

func getKey(affixArr ...string) string {
	var sb strings.Builder

	sb.WriteString(config.GetConfig().ServiceKey)
	for _, affix := range affixArr {
		sb.WriteString(":")
		sb.WriteString(affix)
	}

	return sb.String()
}
