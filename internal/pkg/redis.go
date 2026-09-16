package pkg

import (
	"fmt"

	"loginserver/internal"
)

func WrapRedisLoginTokenKey(options *internal.Options, accountId string) string {
	prefix := fmt.Sprintf("%s/%s:%s", options.Belong, options.ServerType, options.Zone)
	return fmt.Sprintf("%s:loginToken:%s", prefix, accountId)
}

func WrapRedisAccountKey(options *internal.Options, openId string, ptId int32) string {
	prefix := fmt.Sprintf("%s/%s:%s", options.Belong, options.ServerType, options.Zone)
	return fmt.Sprintf("%s:account:%s:%d", prefix, openId, ptId)
}
