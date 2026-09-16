package internal

import (
	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/component"
	"github.com/streasure/util/uetcd"
	"github.com/streasure/util/uredis"
)

func InitBaseComponent(container *component.Container, config *Config) {
	// Redis Component
	if config.Redis.Address != "" {
		network := config.Redis.Network
		if network == "" {
			network = "tcp"
		}
		container.Add(uredis.NewStandaloneComponent(&redis.Options{
			Addr:     config.Redis.Address,
			Network:  network,
			DB:       config.Redis.DB,
			Password: config.Redis.Password,
		}))
	}

	// Etcd Component (Registration + Discovery)
	serviceID := config.Belong + "/" + config.ServerType + ":" + config.Zone
	instanceID := config.ServerId
	grpcAddr := config.Ports.GrpcServiceAddr
	endpoints := config.Etcd.Endpoints
	if len(endpoints) == 0 && config.Etcd.Endpoint != "" {
		endpoints = []string{config.Etcd.Endpoint}
	}

	etcdComp := uetcd.New(uetcd.ComponentConfig{
		Enabled: true,
		Etcd: uetcd.Config{
			Endpoints:     endpoints,
			ServicePrefix: config.Etcd.ServicePrefix,
		},
		Registration: uetcd.RegistrationConfig{
			Enabled:    true,
			ServiceID:  serviceID,
			InstanceID: instanceID,
			Address:    grpcAddr,
			LeaseTTL:   config.Etcd.LeaseTTL,
		},
		Discovery: uetcd.DiscoveryConfig{
			Enabled:   true,
			ServiceID: serviceID,
		},
	})
	container.Add(etcdComp)
}
