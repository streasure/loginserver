package config

import (
	"errors"
	"fmt"

	"github.com/streasure/protocol/enums"
	"github.com/streasure/util/uconfig"
	"github.com/streasure/util/uetcd"
	"github.com/streasure/util/uload"
	"github.com/streasure/util/uperf"
	"github.com/streasure/util/uredis"
)

type Config struct {
	Belong     string `validate:"required"`
	ServerType string `validate:"required"`
	Zone       string `validate:"required"`
	ServerId   string `validate:"required"`
	Ports      Ports  `validate:"required"`
	// Redis redis 段完整配置（连接池/超时 + 实例连接信息），校验在 uredis.Config.Validate；
	// 组件由 uredis.Init(conf.Redis) 一行创建并注册全局。
	Redis  uredis.Config  `yaml:"redis" validate:"required"`
	Etcd   uetcd.EtcdInfo `validate:"required"`
	Limits LimitInfo      `validate:"required"`
	Perf   uperf.Config   `validate:"required"`
	Load   uload.Config   `yaml:"load"` // 自适应准入控制（整机 CPU/内存边界），随 ugin.WithLoad 接入，全 0 边界时惰性不生效

	ServiceKey     string
	LoginServerKey string
}

type Ports struct {
	HttpAddr        int `validate:"required"`
	GrpcServiceAddr int `validate:"required"`
	PprofPort       int `validate:"required"`
}

type LimitInfo struct {
	LoginTokenExpireSeconds int64  `default:"86400"`
	ValidateTokenCacheTtl   string `default:"1s"`
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
	if err := cfg.Redis.Validate(); err != nil {
		return err
	}
	if cfg.Limits.LoginTokenExpireSeconds <= 0 {
		return errors.New("limits.loginTokenExpireSeconds must be positive")
	}
	return nil
}
