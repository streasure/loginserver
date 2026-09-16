package component

import (
	ucomponent "github.com/streasure/util/component"
	"github.com/streasure/util/uetcd"
	"loginserver/internal/config"
)

func AddEtcd(container *ucomponent.Container) {
	cfg := config.GetConfig()
	endpoints := cfg.Etcd.Endpoints
	if len(endpoints) == 0 && cfg.Etcd.Endpoint != "" {
		endpoints = []string{cfg.Etcd.Endpoint}
	}

	serviceID := cfg.Belong + "/" + cfg.ServerType + ":" + cfg.Zone
	container.Add(uetcd.New(uetcd.ComponentConfig{
		Etcd: uetcd.Config{
			Endpoints:     endpoints,
			ServicePrefix: cfg.Etcd.ServicePrefix,
		},
		Registration: uetcd.RegistrationConfig{
			ServiceID:  serviceID,
			InstanceID: cfg.ServerId,
			Address:    cfg.Ports.GrpcServiceAddr,
			LeaseTTL:   cfg.Etcd.LeaseTTL,
		},
		Discovery: uetcd.DiscoveryConfig{
			ServiceID: serviceID,
		},
	}))
}
