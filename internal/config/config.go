package config

import (
	"errors"
	"fmt"

	"github.com/streasure/util/uconfig"
)

type Config struct {
	Belong     string    `yaml:"belong"`
	ServerType string    `yaml:"serverType"`
	Zone       string    `yaml:"zone"`
	ServerId   string    `yaml:"serverId"`
	Ports      Ports     `yaml:"ports"`
	Redis      RedisInfo `yaml:"redis"`
	Etcd       EtcdInfo  `yaml:"etcd"`
	Limits     LimitInfo `yaml:"limits"`
	Perf       PerfInfo  `yaml:"perf"`

	ServiceKey string
}

type Ports struct {
	HttpAddr        int `yaml:"httpAddr"`
	GrpcServiceAddr int `yaml:"grpcServiceAddr"`
	PprofPort       int `yaml:"pprofPort"`
}

type RedisInfo struct {
	Address         string `yaml:"address"`
	DB              int    `yaml:"db"`
	Network         string `yaml:"network"`
	Password        string `yaml:"password"`
	ReadTimeoutSec  int64  `yaml:"readTimeoutSec"`
	WriteTimeoutSec int64  `yaml:"writeTimeoutSec"`
}

type EtcdInfo struct {
	Endpoints     []string `yaml:"endpoints"`
	ServicePrefix string   `yaml:"servicePrefix"`
	LeaseTTL      string   `yaml:"leaseTTL"`
}

type LimitInfo struct {
	LoginTokenExpireSeconds int64  `yaml:"loginTokenExpireSeconds"`
	ValidateTokenCacheTtl   string `yaml:"validateTokenCacheTtl"`
}

type PerfInfo struct {
	GcPercent          int `yaml:"gcPercent"`
	MemoryLimitPercent int `yaml:"memoryLimitPercent"`
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
	if cfg.Redis.Address == "" {
		return errors.New("redis.address is required")
	}
	if cfg.Limits.LoginTokenExpireSeconds <= 0 {
		return errors.New("limits.loginTokenExpireSeconds must be positive")
	}
	return nil
}
