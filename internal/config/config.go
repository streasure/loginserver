package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/streasure/protocol/enums"
	"github.com/streasure/util/uconfig"
	"github.com/streasure/util/uredis"
)

type Config struct {
	Belong          string                         `validate:"required"`
	ServerType      string                         `validate:"required"`
	Zone            string                         `validate:"required"`
	ServerId        string                         `validate:"required"`
	Ports           Ports                          `validate:"required"`
	Redis           map[string]uredis.URedisConfig `validate:"required"`
	Etcd            EtcdInfo                       `validate:"required"`
	Limits          LimitInfo                      `validate:"required"`
	Perf            PerfInfo                       `validate:"required"`
	ReadTimeoutSec  int64                          `validate:"required"`
	WriteTimeoutSec int64                          `validate:"required"`

	// Redis 连接池参数（对所有实例生效）；数值 0 或时长为空表示沿用 uredis 默认值。
	// 注意：uconfig 不支持 default tag，未配置时字段为零值，由组件构造侧兜底。
	RedisMinIdleConns int    `validate:"required"` // 最小空闲连接数
	RedisMaxIdleConns int    `validate:"required"` // 最大空闲连接数
	RedisMaxOpenConns int    `validate:"required"` // 最大 open 连接数（对应 go-redis PoolSize）
	RedisMaxIdleTime  string `validate:"required"` // 连接最大空闲时间（如 "300s"），需小于 redis server 的 timeout
	RedisMaxLifeTime  string `validate:"required"` // 连接最大生命周期（如 "3600s"），需根据 redis server 的 timeout 来设置

	ServiceKey     string
	LoginServerKey string
}

type Ports struct {
	HttpAddr        int `validate:"required"`
	GrpcServiceAddr int `validate:"required"`
	PprofPort       int `validate:"required"`
}

type EtcdInfo struct {
	Endpoints     []string `validate:"required"`
	ServicePrefix string   `validate:"required"`
	LeaseTTL      string   `default:"10s"`
}

type LimitInfo struct {
	LoginTokenExpireSeconds int64  `default:"86400"`
	ValidateTokenCacheTtl   string `default:"1s"`
}

type PerfInfo struct {
	GcPercent          int `default:"100"`
	MemoryLimitPercent int `default:"90"`
}

var _defaultConfig = &Config{}

func GetConfig() *Config {
	return _defaultConfig
}

func LoadConfig(configFile ...string) error {
	cfg, err := uconfig.Load[Config](configFile...)
	if err != nil {
		return err
	}
	if err := validate(cfg); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	cfg.ServiceKey = cfg.Belong + "/" + cfg.ServerType + ":" + cfg.Zone
	loginServerKey := enums.ServerType_name[int32(enums.ServerType_SERVER_TYPE_LOGINSERVER)]
	cfg.LoginServerKey = cfg.Belong + "/" + loginServerKey + ":" + cfg.Zone
	_defaultConfig = cfg
	return nil
}

func validate(cfg *Config) error {
	if cfg.Belong == "" {
		return errors.New("belong is required")
	}
	if cfg.ServerType == "" {
		return errors.New("serverType is required")
	}
	if cfg.Zone == "" {
		return errors.New("zone is required")
	}
	if cfg.Ports.HttpAddr <= 0 {
		return errors.New("ports.httpAddr must be positive")
	}
	if cfg.Ports.GrpcServiceAddr < 0 {
		return errors.New("ports.grpcServiceAddr must be non-negative")
	}
	if cfg.Ports.PprofPort <= 0 {
		return errors.New("ports.pprofPort must be positive")
	}
	if cfg.Ports.HttpAddr == cfg.Ports.GrpcServiceAddr {
		return errors.New("ports.httpAddr and ports.grpcServiceAddr must differ")
	}
	if cfg.Ports.HttpAddr == cfg.Ports.PprofPort {
		return errors.New("ports.httpAddr and ports.pprofPort must differ")
	}
	if cfg.Ports.GrpcServiceAddr == cfg.Ports.PprofPort {
		return errors.New("ports.grpcServiceAddr and ports.pprofPort must differ")
	}
	if len(cfg.Redis) == 0 {
		return errors.New("redis config is required")
	}
	for name, rc := range cfg.Redis {
		if rc.Address == "" {
			return fmt.Errorf("redis[%s].address is required", name)
		}
	}
	if cfg.Limits.LoginTokenExpireSeconds <= 0 {
		return errors.New("limits.loginTokenExpireSeconds must be positive")
	}
	// duration 配置非空时必须可解析（格式如 "300s"、"1h"），启动早失败
	if cfg.RedisMaxIdleTime != "" {
		if _, err := time.ParseDuration(cfg.RedisMaxIdleTime); err != nil {
			return fmt.Errorf("RedisMaxIdleTime(%s) invalid: %w", cfg.RedisMaxIdleTime, err)
		}
	}
	if cfg.RedisMaxLifeTime != "" {
		if _, err := time.ParseDuration(cfg.RedisMaxLifeTime); err != nil {
			return fmt.Errorf("RedisMaxLifeTime(%s) invalid: %w", cfg.RedisMaxLifeTime, err)
		}
	}
	return nil
}
