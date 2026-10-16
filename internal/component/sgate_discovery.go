package component

import (
	"fmt"

	"github.com/streasure/loginserver/internal/config"

	"github.com/streasure/protocol/enums"
	"github.com/streasure/util/uetcd"
)

// sgateDiscoveryWrapper 包装 uetcd.Component 以使用独立组件名（容器按 Name 注册，
// uetcd.Component 固定返回 "etcd"，与 NewEtcdComponent 的注册名冲突）。
type sgateDiscoveryWrapper struct {
	*uetcd.Component
}

func (s *sgateDiscoveryWrapper) Name() string { return "sgate-discovery" }

// Inner 暴露底层 uetcd.Component（供 SgateClientPool 做 ServiceSet/OnServiceChange 适配）
func (s *sgateDiscoveryWrapper) Inner() *uetcd.Component { return s.Component }

// NewSgateDiscoveryComponent 创建 SGATE 服务发现组件（仅发现，不注册）。
// loginserver 经此发现 sgate 实例并下发 Kick RPC（单向 Kick 模型）。
// ServiceID 与 sgate 自注册格式一致：{belong}/{SGATE}:{zone}
func NewSgateDiscoveryComponent() (*sgateDiscoveryWrapper, error) {
	cfg := config.GetConfig()

	if len(cfg.Etcd.Endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints is empty")
	}

	sgateType := enums.ServerType_name[int32(enums.ServerType_SERVER_TYPE_SGATE)]
	serviceKey := cfg.Belong + "/" + sgateType + ":" + cfg.Zone

	comp := uetcd.New(uetcd.ComponentConfig{
		Etcd: uetcd.Config{
			Endpoints:     cfg.Etcd.Endpoints,
			ServicePrefix: cfg.Etcd.ServicePrefix,
		},
		Discovery: uetcd.DiscoveryConfig{
			ServiceID: serviceKey,
		},
	})
	return &sgateDiscoveryWrapper{Component: comp}, nil
}
