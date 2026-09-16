package component

import (
	"loginserver/internal/config"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/uredis"
)

func NewRedisComponent() *uredis.StandaloneComponent {
	cfg := config.GetConfig()
	if len(cfg.Redis.Address) == 0 {
		return nil
	}

	network := cfg.Redis.Network
	if network == "" {
		network = "tcp"
	}
	return uredis.NewStandaloneComponent(&redis.Options{
		Addr:         cfg.Redis.Address,
		Network:      network,
		DB:           cfg.Redis.DB,
		Password:     cfg.Redis.Password,
		ReadTimeout:  time.Duration(cfg.RedisReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.RedisWriteTimeoutSec) * time.Second,
	})
}
