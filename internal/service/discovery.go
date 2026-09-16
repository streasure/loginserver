package service

import (
	"context"
	"fmt"

	"github.com/streasure/util/cluster"
	"github.com/streasure/util/uetcd"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	loginproto "github.com/streasure/protocol/loginserver"
)

var globalEtcdComp *uetcd.Component

func InitEtcdComponent(comp *uetcd.Component) {
	globalEtcdComp = comp
	cluster.InitEtcdDiscoveryClient(comp, "", "")
}

func GetLoginServiceClient(ctx context.Context, belong, zone string) (loginproto.LoginServiceClient, error) {
	return cluster.GetClient[loginproto.LoginServiceClient](
		ctx,
		belong,
		zone,
		"LoginServer",
		func(ctx context.Context, addr string) (loginproto.LoginServiceClient, error) {
			conn, err := grpc.Dial(addr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				return nil, fmt.Errorf("grpc dial failed: %w", err)
			}
			return loginproto.NewLoginServiceClient(conn), nil
		},
	)
}

func GetLoginServiceAddr(belong, zone string) (string, error) {
	return cluster.GetServiceAddr(belong, zone, "LoginServer")
}
