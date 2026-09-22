package component

import (
	"time"

	"loginserver/internal/config"

	"github.com/streasure/util/uredis"
)

// NewRedisComponent 创建 Redis 组件（单实例，SceneServer 风格的函数式选项）。
// 实例名固定为 "login"（对应 loginserver.yaml redis 段的键名）；
// 组件自身实现 uredis.Client 接口，Init 后可直接当 Redis 客户端使用。
func NewRedisComponent() *uredis.URedisComponent {
	cfg := config.GetConfig()

	opts := []uredis.Option{
		uredis.WithConfigs(cfg.Redis),
		uredis.WithReadTimeout(time.Duration(cfg.ReadTimeoutSec) * time.Second),
		uredis.WithWriteTimeout(time.Duration(cfg.WriteTimeoutSec) * time.Second),
	}
	// 连接池参数：数值 0 或时长为空表示沿用 uredis 默认值，不显式覆盖
	if cfg.RedisMinIdleConns > 0 {
		opts = append(opts, uredis.WithMinIdleConns(cfg.RedisMinIdleConns))
	}
	if cfg.RedisMaxIdleConns > 0 {
		opts = append(opts, uredis.WithMaxIdleConns(cfg.RedisMaxIdleConns))
	}
	if cfg.RedisMaxOpenConns > 0 {
		// "最大 open 连接数" 对应 go-redis 的 PoolSize（池内最大连接数）
		opts = append(opts, uredis.WithPoolSize(cfg.RedisMaxOpenConns))
	}
	if d, err := time.ParseDuration(cfg.RedisMaxIdleTime); err == nil {
		opts = append(opts, uredis.WithMaxIdleTime(d))
	}
	if d, err := time.ParseDuration(cfg.RedisMaxLifeTime); err == nil {
		opts = append(opts, uredis.WithMaxLifeTime(d))
	}

	return uredis.InitRedisComponent(opts...)
}
