package pkg

import (
	"strconv"

	"loginserver/internal/config"
)

// WrapRedisLoginTokenKey 拼接 loginToken 的完整 redis key。
// 使用字符串拼接而非 fmt.Sprintf，避免每次调用的反射开销（热路径）
func WrapRedisLoginTokenKey(cfg *config.Config, accountId string) string {
	return cfg.Belong + "/" + cfg.ServerType + ":" + cfg.Zone + ":loginToken:" + accountId
}

// WrapRedisAccountKey 拼接 account 绑定的完整 redis key
func WrapRedisAccountKey(cfg *config.Config, openId string, ptId int32) string {
	return cfg.Belong + "/" + cfg.ServerType + ":" + cfg.Zone + ":account:" + openId + ":" + strconv.Itoa(int(ptId))
}
