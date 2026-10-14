package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	loginproto "github.com/streasure/protocol/loginserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// grpc_admin：模拟 sgate 调用 loginserver 的 ACL gRPC 接口（黑名单 IP / 封禁角色）
//
//	go run ./tools/grpc_admin -mode blacklist -ip 1.2.3.4 -remark drill
//	go run ./tools/grpc_admin -mode banrole -role <accountId> -reason drill
func main() {
	addr := flag.String("addr", "127.0.0.1:10002", "loginserver grpc address")
	mode := flag.String("mode", "blacklist", "blacklist | banrole")
	ip := flag.String("ip", "", "blacklist ip")
	remark := flag.String("remark", "", "blacklist remark")
	role := flag.String("role", "", "banned role id (accountId)")
	reason := flag.String("reason", "", "ban reason")
	operator := flag.String("operator", "sgate", "operator")
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Printf("dial error: %v\n", err)
		return
	}
	defer conn.Close()

	client := loginproto.NewLoginServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch *mode {
	case "blacklist":
		fmt.Printf("AddBlacklistIP addr=%s ip=%s remark=%s\n", *addr, *ip, *remark)
		ack, err := client.AddBlacklistIP(ctx, &loginproto.AddBlacklistIPReq{
			Ip: *ip, Remark: *remark, Operator: *operator,
		})
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		fmt.Printf("added: %v\n", ack.Added)
	case "banrole":
		fmt.Printf("AddBannedRole addr=%s roleId=%s reason=%s\n", *addr, *role, *reason)
		ack, err := client.AddBannedRole(ctx, &loginproto.AddBannedRoleReq{
			RoleId: *role, Reason: *reason, Operator: *operator,
		})
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		fmt.Printf("added: %v\n", ack.Added)
	default:
		fmt.Printf("unknown mode: %s\n", *mode)
	}
}
