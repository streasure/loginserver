// Package rmodel Redis 数据访问层，封装所有 Redis key 构建和命令执行。
// 业务层直接调用包级函数完成 Redis 操作，不直接接触 Redis 客户端。
package rmodel

import (
	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/uredis"
	"strings"

	"github.com/streasure/loginserver/internal/config"
)

// redisName rmodel 使用的 redis 实例名（loginserver.yaml redis.instances 段键名）
const redisName = "rdb"

func getKey(affixArr ...string) string {
	var sb strings.Builder

	sb.WriteString(config.GetConfig().ServiceKey)
	for _, affix := range affixArr {
		sb.WriteString(":")
		sb.WriteString(affix)
	}

	return sb.String()
}

func GetRedisClient() (redis.Cmdable, error) {
	return uredis.Client(redisName)
}
