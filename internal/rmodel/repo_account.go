package rmodel

import (
	"context"
	"errors"
	"strconv"

	"github.com/streasure/util/tlog"

	redis "github.com/redis/go-redis/v9"
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
	key := m.genAccountKey(openId, ptId)
	cli, err := GetRedisClient()
	if err != nil {
		return "", err
	}
	accountId, err := cli.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return accountId, nil
}

// SetAccountNX 绑定 openId+ptId → accountId（SETNX，永不过期）。
// created=true 本次写入成功；false 表示已存在绑定（并发首登竞争失败，调用方应读取赢家绑定）。
func (m *AccountModel) SetAccountNX(ctx context.Context, openId string, ptId int32, accountId string) (bool, error) {
	key := m.genAccountKey(openId, ptId)
	cli, err := GetRedisClient()
	if err != nil {
		return false, err
	}
	created, err := cli.SetNX(ctx, key, accountId, 0).Result()
	if err != nil {
		tlog.Error(ctx, "redis setnx account failed openId:%s ptId:%d account:%s err:%v", openId, ptId, accountId, err)
		return false, err
	}
	return created, nil
}
