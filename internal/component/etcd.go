package component

import (
	"fmt"
	"loginserver/internal/config"
	"loginserver/internal/rpc"

	"github.com/streasure/util/uetcd"
)

// NewEtcdComponent 创建 etcd 注册中心组件。
// 服务身份（ServiceKey/ServerId）与通告地址（AdvertiseAddr）取自 rpcServer
// 内的 gRPC 服务器，与 gRPC 服务保持单一数据源；gRPC 服务器未创建时直接报错。
// etcd 组件由 util 默认排在所有业务组件之后启动（Order 最大），注册时业务已就绪
func NewEtcdComponent(rpcServer *rpc.LoginGrpcServer) (*uetcd.Component, error) {
	cfg := config.GetConfig()

	grpcSrv := rpcServer.GrpcServer()
	if grpcSrv == nil {
		return nil, fmt.Errorf("etcd registration requires grpc server, but it is not created (check ports.grpcServiceAddr)")
	}

	serviceKey := grpcSrv.ServiceKey()
	comp := uetcd.New(uetcd.ComponentConfig{
		Etcd: uetcd.Config{
			Endpoints:     cfg.Etcd.Endpoints,
			ServicePrefix: cfg.Etcd.ServicePrefix,
		},
		Registration: uetcd.RegistrationConfig{
			ServiceID:  serviceKey,
			InstanceID: grpcSrv.ServerId(),
			Address:    grpcSrv.AdvertiseAddr(),
			LeaseTTL:   cfg.Etcd.LeaseTTL,
		},
		Discovery: uetcd.DiscoveryConfig{
			ServiceID: serviceKey,
		},
	})
	return comp, nil
}
