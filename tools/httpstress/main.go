package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	target := flag.String("target", "http://127.0.0.1:10001/api/v1/version", "HTTP endpoint")
	concurrency := flag.Int("c", 100, "concurrent connections")
	duration := flag.Duration("d", 10*time.Second, "test duration")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration")
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	transport := &http.Transport{
		MaxIdleConns:        *concurrency,
		MaxIdleConnsPerHost: *concurrency,
		MaxConnsPerHost:     *concurrency,
		IdleConnTimeout:     60 * time.Second,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	// warmup: send requests to fill connection pool and stabilize GC
	fmt.Printf("warming up for %s...\n", *warmup)
	warmupDeadline := time.Now().Add(*warmup)
	var warmupWg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		warmupWg.Add(1)
		go func() {
			defer warmupWg.Done()
			body := []byte("{}")
			for time.Now().Before(warmupDeadline) {
				req, err := http.NewRequest("POST", *target, bytes.NewReader(body))
				if err != nil {
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
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
		go func() {
			defer wg.Done()
			body := []byte("{}")
			localLatencies := make([]int64, 0, 100000)
			for time.Now().Before(deadline) {
				started := time.Now()
				req, err := http.NewRequest("POST", *target, bytes.NewReader(body))
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				lat := time.Since(started).Nanoseconds()
				atomic.AddInt64(&totalLatencyNs, lat)
				localLatencies = append(localLatencies, lat)
				atomic.AddInt64(&completed, 1)
			}
			mu.Lock()
			latencies = append(latencies, localLatencies...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	transport.CloseIdleConnections()

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

	fmt.Printf("concurrency=%d duration=%s\n", *concurrency, *duration)
	fmt.Printf("total=%d completed=%d failed=%d\n", total, completed, failed)
	fmt.Printf("rps=%.0f avg=%.2fms p50=%.2fms p95=%.2fms p99=%.2fms p99.9=%.2fms max=%.2fms\n",
		rps, avgMs,
		float64(p50)/float64(time.Millisecond),
		float64(p95)/float64(time.Millisecond),
		float64(p99)/float64(time.Millisecond),
		float64(p999)/float64(time.Millisecond),
		float64(max)/float64(time.Millisecond))
}
