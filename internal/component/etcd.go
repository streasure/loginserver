package component

import (
	"fmt"
	"loginserver/internal/config"

	"github.com/streasure/util/netutil"
	"github.com/streasure/util/uetcd"
)

// NewEtcdComponent 创建 etcd 注册中心组件。
// 服务身份与通告地址直接从 config 构建，不依赖其他组件。
func NewEtcdComponent() (*uetcd.Component, error) {
	cfg := config.GetConfig()

	if len(cfg.Etcd.Endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints is empty")
	}

	serviceKey := cfg.LoginServerKey
	advertiseAddr := netutil.LocalIP() + fmt.Sprintf(":%d", cfg.Ports.GrpcServiceAddr)

	comp := uetcd.New(uetcd.ComponentConfig{
		Etcd: uetcd.Config{
			Endpoints:     cfg.Etcd.Endpoints,
			ServicePrefix: cfg.Etcd.ServicePrefix,
		},
		Registration: uetcd.RegistrationConfig{
			ServiceID:  serviceKey,
			InstanceID: cfg.ServerId,
			Address:    advertiseAddr,
			LeaseTTL:   cfg.Etcd.LeaseTTL,
		},
		Discovery: uetcd.DiscoveryConfig{
			ServiceID: serviceKey,
		},
	})
	return comp, nil
}
