package main

import (
	"context"
	"fmt"
	"os"
	"time"

	loginproto "github.com/streasure/protocol/loginserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	target := "127.0.0.1:10002"
	accountID := "4605caae66d644488caf21b420fafaaa"
	loginToken := "fb20959409dc48c1bc68838d62d496f5"

	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	if len(os.Args) > 2 {
		accountID = os.Args[2]
	}
	if len(os.Args) > 3 {
		loginToken = os.Args[3]
	}

	fmt.Printf("dial %s\n", target)
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		fmt.Printf("dial error: %v\n", err)
		return
	}
	defer conn.Close()

	client := loginproto.NewLoginServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	fmt.Printf("ValidateLoginToken  accountId=%s  loginToken=%s\n", accountID, loginToken)
	ack, err := client.ValidateLoginToken(ctx, &loginproto.ValidateLoginTokenReq{
		AccountId:  accountID,
		LoginToken: loginToken,
	})
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("valid: %v\n", ack.Valid)
}
