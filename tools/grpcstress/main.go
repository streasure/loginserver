package main

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	loginproto "github.com/streasure/protocol/loginserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	target := flag.String("target", "127.0.0.1:10002", "gRPC target")
	concurrency := flag.Int("c", 100, "concurrent workers")
	connections := flag.Int("conn", 0, "shared connections (0 = one per worker)")
	duration := flag.Duration("d", 10*time.Second, "test duration")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration")
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	numConns := *connections
	if numConns <= 0 {
		numConns = *concurrency
	}

	conns := make([]*grpc.ClientConn, numConns)
	clients := make([]loginproto.LoginServiceClient, numConns)
	for i := 0; i < numConns; i++ {
		conn, err := grpc.Dial(*target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4*1024*1024)),
		)
		if err != nil {
			fmt.Printf("dial error: %v\n", err)
			return
		}
		conns[i] = conn
		clients[i] = loginproto.NewLoginServiceClient(conn)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	// warmup
	fmt.Printf("warming up for %s (connections=%d, workers=%d)...\n", *warmup, numConns, *concurrency)
	warmupDeadline := time.Now().Add(*warmup)
	var warmupWg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		warmupWg.Add(1)
		go func(idx int) {
			defer warmupWg.Done()
			connIdx := idx % numConns
			for time.Now().Before(warmupDeadline) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				clients[connIdx].ValidateLoginToken(ctx, &loginproto.ValidateLoginTokenReq{
					AccountId:  "warmup",
					LoginToken: "warmup",
				})
				cancel()
			}
		}(i)
	}
	warmupWg.Wait()
	runtime.GC()

	// actual test
	var completed, failed, totalLatencyNs int64
	latencies := make([]int64, 0, 5000000)
	var mu sync.Mutex

	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			connIdx := idx % numConns
			localLatencies := make([]int64, 0, 100000)
			for time.Now().Before(deadline) {
				started := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, callErr := clients[connIdx].ValidateLoginToken(ctx, &loginproto.ValidateLoginTokenReq{
					AccountId:  "stress-account",
					LoginToken: "stress-token",
				})
				cancel()
				lat := time.Since(started).Nanoseconds()
				atomic.AddInt64(&totalLatencyNs, lat)
				localLatencies = append(localLatencies, lat)
				if callErr != nil {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&completed, 1)
				}
			}
			mu.Lock()
			latencies = append(latencies, localLatencies...)
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	total := atomic.LoadInt64(&completed) + atomic.LoadInt64(&failed)
	if total == 0 {
		fmt.Println("no requests completed")
		return
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]
	p999 := latencies[len(latencies)*999/1000]
	max := latencies[len(latencies)-1]
	avgMs := float64(atomic.LoadInt64(&totalLatencyNs)) / float64(total) / float64(time.Millisecond)
	rps := float64(total) / duration.Seconds()

	fmt.Printf("concurrency=%d connections=%d duration=%s\n", *concurrency, numConns, *duration)
	fmt.Printf("total=%d completed=%d failed=%d\n", total, completed, failed)
	fmt.Printf("rps=%.0f avg=%.2fms p50=%.2fms p95=%.2fms p99=%.2fms p99.9=%.2fms max=%.2fms\n",
		rps, avgMs,
		float64(p50)/float64(time.Millisecond),
		float64(p95)/float64(time.Millisecond),
		float64(p99)/float64(time.Millisecond),
		float64(p999)/float64(time.Millisecond),
		float64(max)/float64(time.Millisecond))
}
