package component

import (
	"fmt"

	"github.com/streasure/loginserver/internal/config"

	"github.com/streasure/protocol/enums"
	"github.com/streasure/util/uetcd"
)

// NewSgateDiscoveryComponent 创建 SGATE 服务发现组件（仅发现，不注册）。
// loginserver 经此发现 sgate 实例并下发 Kick RPC（单向 Kick 模型）。
// ServiceID 与 sgate 自注册格式一致：{belong}/{SGATE}:{zone}
func NewSgateDiscoveryComponent() (*uetcd.Component, error) {
	cfg := config.GetConfig()

	if len(cfg.Etcd.Endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints is empty")
	}

	sgateType := enums.ServerType_name[int32(enums.ServerType_SERVER_TYPE_SGATE)]
	serviceKey := cfg.Belong + "/" + sgateType + ":" + cfg.Zone

	return uetcd.New(uetcd.ComponentConfig{
		Etcd: uetcd.Config{
			Endpoints:     cfg.Etcd.Endpoints,
			ServicePrefix: cfg.Etcd.ServicePrefix,
		},
		Discovery: uetcd.DiscoveryConfig{
			ServiceID: serviceKey,
		},
	}), nil
}
