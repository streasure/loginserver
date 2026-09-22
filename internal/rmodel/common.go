// Package rmodel Redis 数据访问层，封装所有 Redis key 构建和命令执行。
// 业务层直接调用包级函数完成 Redis 操作，不直接接触 Redis 客户端。
package rmodel

import (
	"errors"
	"loginserver/internal/config"
	"strconv"

	"github.com/streasure/util/component"
	"github.com/streasure/util/uredis"
)

// ErrRedisUnavailable Redis 客户端未就绪
var ErrRedisUnavailable = errors.New("redis client unavailable")

// redisCli 返回全局 Redis 客户端（从 component 容器获取）
func redisCli() (uredis.Client, error) {
	if comp := component.Get[*uredis.StandaloneComponent](); comp != nil {
		return comp, nil
	}
	return nil, ErrRedisUnavailable
}

// accountKey 拼接 account 绑定的 Redis key：
// {belong}/{serverType}:{zone}:account:{openId}:{ptId}
func accountKey(openId string, ptId int32) string {
	return config.GetConfig().ServiceKey + "account:" + openId + ":" + strconv.Itoa(int(ptId))
}

// loginTokenKey 拼接 loginToken 的 Redis key：
// {belong}/{serverType}:{zone}:loginToken:{accountId}
func loginTokenKey(accountId string) string {
	return config.GetConfig().ServiceKey + "loginToken:" + accountId
}
