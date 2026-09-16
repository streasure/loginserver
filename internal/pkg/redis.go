package pkg

import (
	"fmt"
	"loginserver/internal/config"
)

func WrapRedisLoginTokenKey(cfg *config.Config, accountId string) string {
	prefix := fmt.Sprintf("%s/%s:%s", cfg.Belong, cfg.ServerType, cfg.Zone)
	return fmt.Sprintf("%s:loginToken:%s", prefix, accountId)
}

func WrapRedisAccountKey(cfg *config.Config, openId string, ptId int32) string {
	prefix := fmt.Sprintf("%s/%s:%s", cfg.Belong, cfg.ServerType, cfg.Zone)
	return fmt.Sprintf("%s:account:%s:%d", prefix, openId, ptId)
}
