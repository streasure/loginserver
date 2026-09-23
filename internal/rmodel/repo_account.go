package rmodel

import (
	"context"
	"errors"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/tlog"
)

type AccountModel struct {
}

var accountModel = &AccountModel{}

func GetAccountModel() *AccountModel {
	return accountModel
}

// genAccountKey 拼接 account 绑定的 Redis key：
// {belong}/{serverType}:{zone}:account:{openId}:{ptId}
func (m *AccountModel) genAccountKey(openId string, ptId int32) string {
	return getKey("account", openId, strconv.Itoa(int(ptId)))
}

// GetAccount 根据 openId+ptId 获取已绑定的 accountId，未绑定时返回 ("", nil)
func (m *AccountModel) GetAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	cli, err := GetRedisCli()
	if err != nil {
		return "", err
	}

	key := m.genAccountKey(openId, ptId)
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
func (m *AccountModel) SetAccount(ctx context.Context, openId string, ptId int32, accountId string) error {
	cli, err := GetRedisCli()
	if err != nil {
		return err
	}

	key := m.genAccountKey(openId, ptId)
	if err = cli.Set(ctx, key, accountId, 0).Err(); err != nil {
		tlog.Error(ctx, "redis set account failed openId:%s ptId:%d account:%s err:%v", openId, ptId, accountId, err)
		return err
	}
	return nil
}
