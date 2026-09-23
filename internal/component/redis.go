package component

import (
	"time"

	"loginserver/internal/config"

	"github.com/streasure/util/uredis"
)

// NewRedisComponent 创建 Redis 组件并注册为 uredis 全局实例。
// 实例配置来自 loginserver.yaml 的 redis 段（键为实例名，rmodel 通过 uredis.GetClient("rdb") 取用）；
// 组件接入容器生命周期，Init 时统一建连并 Ping 验证。
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
