package rmodel

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/tlog"
)

// GetAccount 根据 openId+ptId 获取已绑定的 accountId，未绑定时返回 ("", nil)
func GetAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	cli, err := GetRedisCli()
	if err != nil {
		return "", err
	}

	key := accountKey(openId, ptId)
	accountId, err := cli.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return accountId, nil
}

// SetAccount 绑定 openId+ptId → accountId，永不过期
func SetAccount(ctx context.Context, openId string, ptId int32, accountId string) error {
	cli, err := GetRedisCli()
	if err != nil {
		return err
	}

	key := accountKey(openId, ptId)
	if err = cli.Set(ctx, key, accountId, 0).Err(); err != nil {
		tlog.Error(ctx, "redis set account failed openId:%s ptId:%d account:%s err:%v", openId, ptId, accountId, err)
		return err
	}
	return nil
}
