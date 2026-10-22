package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	loginproto "github.com/streasure/protocol/loginserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	target := flag.String("target", "127.0.0.1:10002", "loginserver grpc addr")
	file := flag.String("file", "", "csv file: accountId,loginToken per line")
	timeout := flag.Duration("d", 15*time.Second, "rpc timeout")
	flag.Parse()
	if *file == "" {
		fmt.Fprintln(os.Stderr, "-file required")
		os.Exit(2)
	}
	f, err := os.Open(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()
	var items []*loginproto.ValidateLoginTokenReq
	r := csv.NewReader(f)
	for {
		rec, err := r.Read()
		if err != nil {
			break
		}
		if len(rec) < 2 {
			continue
		}
		items = append(items, &loginproto.ValidateLoginTokenReq{
			AccountId:  strings.TrimSpace(rec[0]),
			LoginToken: strings.TrimSpace(rec[1]),
		})
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "no items")
		os.Exit(1)
	}
	conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	start := time.Now()
	ack, err := loginproto.NewLoginServiceClient(conn).BatchValidateLoginTokens(ctx, &loginproto.BatchValidateLoginTokensReq{
		Items: items,
	})
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc: %v elapsed=%v\n", err, elapsed)
		os.Exit(1)
	}
	trues := 0
	for _, v := range ack.GetValid() {
		if v {
			trues++
		}
	}
	fmt.Printf("items=%d valid=%d elapsed=%v\n", len(items), trues, elapsed)
}
