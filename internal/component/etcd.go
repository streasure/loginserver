package component

import (
	"fmt"
	"loginserver/internal/config"

	"github.com/streasure/util/netutil"
	"github.com/streasure/util/uetcd"
)

func NewEtcdComponent() *uetcd.Component {
	cfg := config.GetConfig()
	endpoints := cfg.Etcd.Endpoints
	if len(endpoints) == 0 && len(cfg.Etcd.Endpoint) > 0 {
		endpoints = []string{cfg.Etcd.Endpoint}
	}

	serviceID := cfg.Belong + "/" + cfg.ServerType + ":" + cfg.Zone
	grpcAddr := fmt.Sprintf("%s:%d", netutil.LocalIP(), cfg.Ports.GrpcServiceAddr)
	return uetcd.New(uetcd.ComponentConfig{
		Etcd: uetcd.Config{
			Endpoints:     endpoints,
			ServicePrefix: cfg.Etcd.ServicePrefix,
		},
		Registration: uetcd.RegistrationConfig{
			ServiceID:  serviceID,
			InstanceID: cfg.ServerId,
			Address:    grpcAddr,
			LeaseTTL:   cfg.Etcd.LeaseTTL,
		},
		Discovery: uetcd.DiscoveryConfig{
			ServiceID: serviceID,
		},
	})
}
