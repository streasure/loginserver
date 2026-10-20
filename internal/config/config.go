package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/streasure/protocol/enums"
	"github.com/streasure/util/uconfig"
	"github.com/streasure/util/uetcd"
	"github.com/streasure/util/ugorm"
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
	Load   uload.Config   `yaml:"load"` // 自适应准入控制（整机 CPU/内存边界），经 LoadComponent 接入 + WithPreMiddleware 注入，全 0 边界时惰性不生效
	Db     ugorm.Config   `yaml:"db"`   // 业务库（白/黑名单、封禁角色、服务器列表），yaml 形态对齐 ugorm.Config
	Acl    AclConfig      `yaml:"acl"`  // 黑白名单/封禁检测的缓存与管理接口配置
	Sgate  SgateConfig    `yaml:"sgate"`

	ServiceKey     string
	LoginServerKey string
}

// SgateConfig loginserver → sgate 的 Kick 联动配置（单向 Kick 模型）
type SgateConfig struct {
	// InternalToken Kick RPC 客户端携带的 token（sgate 侧 kickInternalToken 校验同值）
	InternalToken string `yaml:"internalToken"`
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

// AclConfig ACL（黑白名单/封禁角色）缓存与管理接口配置。
// 缓存采用版本号（gen）token：每次写库 INCR 全局版本，条目携带写入时版本，
// 读侧本地版本快照过期（genRefresh）后强制回源，跨节点脏数据窗口 ≈ genRefresh，
// 条目 TTL（cacheTtl）为 redis 数据丢失/写失败时的兜底上界。
type AclConfig struct {
	CacheTtl   time.Duration `yaml:"cacheTtl" default:"5m"`
	GenRefresh time.Duration `yaml:"genRefresh" default:"2s"`
	AdminToken string        `yaml:"adminToken"` // HTTP 管理接口鉴权头 X-Admin-Token 的期望值
	// TrustedProxies 信任的反代 IP/CIDR（经 ugin.WithTrustedProxies 接入 gin）：
	// 空 = 只信 TCP 对端，防 X-Forwarded-For 伪造；部署在反代/SLB 之后时必须配置
	TrustedProxies []string `yaml:"trustedProxies"`
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
	// 环境变量覆盖管理 token（生产避免明文落盘），在校验前应用
	if v := os.Getenv("LOGINSERVER_ACL_ADMIN_TOKEN"); v != "" {
		cfg.Acl.AdminToken = v
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
	if err := cfg.Db.Validate(); err != nil {
		return err
	}
	if cfg.Acl.CacheTtl <= 0 {
		return errors.New("acl.cacheTtl must be positive")
	}
	if cfg.Acl.GenRefresh <= 0 {
		return errors.New("acl.genRefresh must be positive")
	}
	if cfg.Acl.AdminToken == "" {
		return errors.New("acl.adminToken is required")
	}
	// sgate.internalToken 是封禁踢人链路（KickIP/KickAccounts）的内部鉴权凭据：
	// 空值会导致拨号后全部 Kick 被 sgate 拒绝（封禁踢人静默失效），必须启动即失败
	if cfg.Sgate.InternalToken == "" {
		return errors.New("sgate.internalToken is required")
	}
	for _, proxy := range cfg.Acl.TrustedProxies {
		if strings.Contains(proxy, "/") {
			if _, _, err := net.ParseCIDR(proxy); err != nil {
				return fmt.Errorf("acl.trustedProxies %q is not a valid IP/CIDR", proxy)
			}
		} else if net.ParseIP(proxy) == nil {
			return fmt.Errorf("acl.trustedProxies %q is not a valid IP/CIDR", proxy)
		}
	}
	return nil
}
