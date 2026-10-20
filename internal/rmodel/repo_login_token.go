package rmodel

import (
	"context"
	"time"

	"github.com/streasure/util/tlog"
)

type LoginTokenModel struct {
}

var loginTokenModel = &LoginTokenModel{}

func GetLoginTokenModel() *LoginTokenModel {
	return loginTokenModel
}

// genLoginTokenKey 拼接 loginToken 的 Redis key：
// {belong}/{serverType}:{zone}:loginToken:{accountId}
func (m *LoginTokenModel) genLoginTokenKey(accountId string) string {
	return getKey("loginToken", accountId)
}

// SetLoginToken 写入 loginToken，带过期时间
func (m *LoginTokenModel) SetLoginToken(ctx context.Context, accountId, token string, expireSeconds int64) error {
	key := m.genLoginTokenKey(accountId)
	cli, err := GetRedisClient()
	if err != nil {
		return err
	}
	if err = cli.Set(ctx, key, token, time.Second*time.Duration(expireSeconds)).Err(); err != nil {
		// token 是 bearer 凭证，不落日志
		tlog.Error(ctx, "redis set account:%s login token err:%v", accountId, err)
		return err
	}
	return nil
}

// GetLoginToken 读取 loginToken，key 不存在时返回空串和 redis.Nil
func (m *LoginTokenModel) GetLoginToken(ctx context.Context, accountId string) (string, error) {
	key := m.genLoginTokenKey(accountId)
	cli, err := GetRedisClient()
	if err != nil {
		return "", err
	}
	token, err := cli.Get(ctx, key).Result()
	if err != nil {
		tlog.Debug(ctx, "redis get account:%s login token err:%v", accountId, err)
		return "", err
	}
	return token, nil
}

// GetLoginTokens 批量读取 loginToken（MGET 合并往返）。
// 已过期/不存在的条目不出现在返回 map 中；单次调用建议不超过数百 key。
func (m *LoginTokenModel) GetLoginTokens(ctx context.Context, accountIds []string) (map[string]string, error) {
	if len(accountIds) == 0 {
		return nil, nil
	}
	cli, err := GetRedisClient()
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(accountIds))
	for i, id := range accountIds {
		keys[i] = m.genLoginTokenKey(id)
	}
	vals, err := cli.MGet(ctx, keys...).Result()
	if err != nil {
		tlog.Debug(ctx, "redis mget login tokens count:%d err:%v", len(accountIds), err)
		return nil, err
	}
	out := make(map[string]string, len(vals))
	for i, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out[accountIds[i]] = s
		}
	}
	return out, nil
}
