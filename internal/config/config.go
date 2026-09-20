package config

import (
	"github.com/streasure/util/uconfig"
)

type Config struct {
	Belong   string    `yaml:"belong"`
	ServerType string  `yaml:"serverType"`
	Zone     string    `yaml:"zone"`
	ServerId string    `yaml:"serverId"`
	Ports    Ports     `yaml:"ports"`
	Redis    RedisInfo `yaml:"redis"`
	Etcd     EtcdInfo  `yaml:"etcd"`
	Limits   LimitInfo `yaml:"limits"`
	Perf     PerfInfo  `yaml:"perf"`
}

type Ports struct {
	HttpAddr        int `yaml:"httpAddr"`
	GrpcServiceAddr int `yaml:"grpcServiceAddr"`
	PprofPort       int `yaml:"pprofPort"`
}

type RedisInfo struct {
	Address            string `yaml:"address"`
	DB                 int    `yaml:"db"`
	Network            string `yaml:"network"`
	Password           string `yaml:"password"`
	ReadTimeoutSec     int64  `yaml:"readTimeoutSec"`
	WriteTimeoutSec    int64  `yaml:"writeTimeoutSec"`
}

type EtcdInfo struct {
	Endpoints     []string `yaml:"endpoints"`
	ServicePrefix string   `yaml:"servicePrefix"`
	LeaseTTL      string   `yaml:"leaseTTL"`
}

type LimitInfo struct {
	LoginTokenExpireSeconds int64 `yaml:"loginTokenExpireSeconds"`
	// ValidateTokenCacheTtl token 校验的进程内缓存时长（如 "1s"）。
	// 该缓存消除热路径的 Redis 往返；代价是 token 撤销（删除 Redis key）的生效
	// 延迟上界为该值。设为 "0s" 或不填表示禁用缓存（每次校验都查 Redis）
	ValidateTokenCacheTtl string `yaml:"validateTokenCacheTtl"`
}

// PerfInfo 运行时性能调优参数，进程启动时应用一次（main 中通过 debug 包设置）
type PerfInfo struct {
	// GcPercent 等同于 GOGC 环境变量：触发 GC 的堆增长百分比。
	// 本服务压测显示 300 时吞吐最优（GC 占约 15% CPU）。0 表示保持 Go 默认值 100
	GcPercent int `yaml:"gcPercent"`
	// MemoryLimitPercent Go 堆软内存上限（GOMEMLIMIT 等效），取机器总物理内存的百分比。
	// 接近上限时 GC 会更激进。0 表示不设置；按百分比设计，同一份配置
	// 跨机器部署无需修改，推荐 80-90
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
	_defaultConfig = cfg

	return nil
}
