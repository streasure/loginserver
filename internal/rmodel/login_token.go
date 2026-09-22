package rmodel

import (
	"context"
	"time"

	"github.com/streasure/util/tlog"
)

// SetLoginToken 写入 loginToken，带过期时间
func SetLoginToken(ctx context.Context, accountId, token string, expireSeconds int64) error {
	cli, err := GetRedisCli()
	if err != nil {
		return err
	}

	key := loginTokenKey(accountId)
	if err = cli.Set(ctx, key, token, time.Second*time.Duration(expireSeconds)).Err(); err != nil {
		tlog.Error(ctx, "redis set account:%s login token:%s err:%v", accountId, token, err)
		return err
	}
	return nil
}

// GetLoginToken 读取 loginToken，key 不存在时返回空串和 redis.Nil
func GetLoginToken(ctx context.Context, accountId string) (string, error) {
	cli, err := GetRedisCli()
	if err != nil {
		return "", err
	}

	key := loginTokenKey(accountId)
	token, err := cli.Get(ctx, key).Result()
	if err != nil {
		tlog.Debug(ctx, "redis get account:%s login token err:%v", accountId, err)
		return "", err
	}
	return token, nil
}
