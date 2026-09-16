package internal

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Belong     string         `yaml:"belong"`
	ServerType string         `yaml:"serverType"`
	Zone       string         `yaml:"zone"`
	ServerId   string         `yaml:"serverId"`
	Ports      Ports          `yaml:"ports"`
	Redis      RedisInfo      `yaml:"redis"`
	Etcd       EtcdInfo       `yaml:"etcd"`
	Limits     LimitInfo      `yaml:"limits"`
	ServerList ServerListInfo `yaml:"serverList"`
}

type Ports struct {
	HttpAddr        string `yaml:"httpAddr"`
	GrpcServiceAddr string `yaml:"grpcServiceAddr"`
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

func Load(configFile ...string) error {
	return loadConfigs(configFile...)
}

func loadConfigs(files ...string) error {
	if len(files) == 0 {
		return nil
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		return err
	}

	tmp := &Config{}
	if err := yaml.Unmarshal(data, tmp); err != nil {
		return err
	}

	_defaultConfig = tmp
	return nil
}
