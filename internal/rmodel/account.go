package rmodel

import (
	"context"

	"github.com/streasure/util/tlog"
)

// GetAccount 根据 openId+ptId 获取已绑定的 accountId，未绑定时返回空串
func GetAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	cli, err := redisCli()
	if err != nil {
		return "", err
	}

	key := accountKey(openId, ptId)
	accountId, err := cli.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}
	return accountId, nil
}

// SetAccount 绑定 openId+ptId → accountId，永不过期
func SetAccount(ctx context.Context, openId string, ptId int32, accountId string) error {
	cli, err := redisCli()
	if err != nil {
		return err
	}

	key := accountKey(openId, ptId)
	if err := cli.Set(ctx, key, accountId, 0).Err(); err != nil {
		tlog.Error(ctx, "redis set account failed",
			"openId", openId,
			"ptId", ptId,
			"error", err.Error(),
		)
		return err
	}
	return nil
}
