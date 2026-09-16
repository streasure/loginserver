package component

import (
	"loginserver/internal/config"

	"github.com/redis/go-redis/v9"
	ucomponent "github.com/streasure/util/component"
	"github.com/streasure/util/uredis"
)

func AddRedis(container *ucomponent.Container) {
	cfg := config.GetConfig().Redis
	if cfg.Address == "" {
		return
	}

	network := cfg.Network
	if network == "" {
		network = "tcp"
	}
	container.Add(uredis.NewStandaloneComponent(&redis.Options{
		Addr:     cfg.Address,
		Network:  network,
		DB:       cfg.DB,
		Password: cfg.Password,
	}))
}
