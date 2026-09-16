package config

import (
	"github.com/streasure/util/uconfig"
)

type Config struct {
	Belong               string         `yaml:"belong"`
	ServerType           string         `yaml:"serverType"`
	Zone                 string         `yaml:"zone"`
	ServerId             string         `yaml:"serverId"`
	Ports                Ports          `yaml:"ports"`
	Redis                RedisInfo      `yaml:"redis"`
	Etcd                 EtcdInfo       `yaml:"etcd"`
	Limits               LimitInfo      `yaml:"limits"`
	ServerList           ServerListInfo `yaml:"serverList"`
	RedisReadTimeoutSec  int64          `yaml:"RedisReadTimeoutSec"`
	RedisWriteTimeoutSec int64          `yaml:"RedisWriteTimeoutSec"`
}

type Ports struct {
	HttpAddr        int `yaml:"httpAddr"`
	GrpcServiceAddr int `yaml:"grpcServiceAddr"`
	PprofPort       int `yaml:"pprofPort"`
}

type RedisInfo struct {
	Address  string `yaml:"address"`
	DB       int    `yaml:"db"`
	Network  string `yaml:"network"`
	Password string `yaml:"password"`
}

type EtcdInfo struct {
	Endpoint      string   `yaml:"endpoint"`
	Endpoints     []string `yaml:"endpoints"`
	ServicePrefix string   `yaml:"servicePrefix"`
	LeaseTTL      string   `yaml:"leaseTTL"`
}

type LimitInfo struct {
	LoginTokenExpireSeconds int64 `yaml:"loginTokenExpireSeconds"`
}

type ServerListInfo struct {
	ClientGetServerListUrl string       `yaml:"clientGetServerListUrl"`
	ServerInfos            []ServerInfo `yaml:"serverInfos"`
}

type ServerInfo struct {
	ServerId     int32  `yaml:"serverId"`
	ServerName   string `yaml:"serverName"`
	ServerUrl    string `yaml:"serverUrl"`
	ServerStatus int32  `yaml:"serverStatus"`
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
	_defaultConfig = cfg

	return nil
}
