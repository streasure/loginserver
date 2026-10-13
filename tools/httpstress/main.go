package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type endpoint struct {
	name string
	path string
	body string
}

// registry 全部可压测接口：路径与请求体与服务端 dto 对齐
func registry() map[string]endpoint {
	return map[string]endpoint{
		"login":    {name: "login", path: "/api/v1/login", body: `{"openId":"stress-openid","ptid":1}`},
		"version":  {name: "version", path: "/api/v1/version", body: `{}`},
		"validate": {name: "validate", path: "/api/v1/validate/token", body: `{"accountId":"stress-account","loginToken":"stress-token"}`},
	}
}

func selectEndpoints(spec string) ([]endpoint, error) {
	defaultOrder := []string{"login", "version", "validate"}
	if spec == "" || spec == "all" {
		spec = strings.Join(defaultOrder, ",")
	}
	reg := registry()
	seen := make(map[string]bool)
	out := make([]endpoint, 0, len(defaultOrder))
	for n := range strings.SplitSeq(spec, ",") {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		ep, ok := reg[n]
		if !ok {
			return nil, fmt.Errorf("unknown endpoint %q (available: login, version, validate)", n)
		}
		out = append(out, ep)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no endpoints selected")
	}
	return out, nil
}

func truncate(b []byte) string {
	if len(b) > 256 {
		return string(b[:256]) + "..."
	}
	return string(b)
}

// primeValidate 预登录一次取真实 accountId/token，让 validate 压测命中路径；
// 失败时保留占位凭证（压测未命中路径），不中断测试
func primeValidate(client *http.Client, base string, eps []endpoint) {
	for i := range eps {
		if eps[i].name != "validate" {
			continue
		}
		resp, err := client.Post(base+"/api/v1/login", "application/json",
			bytes.NewReader([]byte(`{"openId":"stress-openid","ptid":1}`)))
		if err != nil {
			fmt.Printf("prime login failed: %v (validate uses miss path)\n", err)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		var ack struct {
			Code int `json:"code"`
			Data struct {
				AccountID  string `json:"accountId"`
				LoginToken string `json:"loginToken"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &ack); err != nil || ack.Code != 0 || ack.Data.AccountID == "" {
			fmt.Printf("prime login unexpected response: %s (validate uses miss path)\n", truncate(raw))
			return
		}
		eps[i].body = fmt.Sprintf(`{"accountId":%q,"loginToken":%q}`, ack.Data.AccountID, ack.Data.LoginToken)
		fmt.Printf("prime login ok account=%s (validate hits token cache)\n", ack.Data.AccountID)
		return
	}
}

type epStat struct {
	completed, bad, failed, totalNs atomic.Int64
}

// doRequest 返回响应状态码（收到响应时），transport 层错误返回 0
func doRequest(client *http.Client, url, body string) (int, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

// runPhase 在 deadline 前按 worker 轮询各自起点分发到各接口；
// stats 为 nil 时为预热阶段（不计数）
func runPhase(client *http.Client, base string, eps []endpoint, concurrency int,
	deadline time.Time, stats []epStat, latMu *sync.Mutex, lat [][]int64) {

	var wg sync.WaitGroup
	for w := range concurrency {
		wg.Go(func() {
			startIdx := w % len(eps)
			local := make([][]int64, len(eps))
			for i := range local {
				local[i] = make([]int64, 0, 8192)
			}
			for j := 0; time.Now().Before(deadline); j++ {
				i := (startIdx + j) % len(eps)
				ep := &eps[i]
				started := time.Now()
				status, err := doRequest(client, base+ep.path, ep.body)
				if err != nil {
					if stats != nil {
						stats[i].failed.Add(1)
					}
					continue
				}
				if stats == nil {
					continue
				}
				latNs := time.Since(started).Nanoseconds()
				if status >= 200 && status < 300 {
					stats[i].completed.Add(1)
				} else {
					stats[i].bad.Add(1)
				}
				stats[i].totalNs.Add(latNs)
				local[i] = append(local[i], latNs)
			}
			if stats != nil {
				latMu.Lock()
				for i := range local {
					lat[i] = append(lat[i], local[i]...)
				}
				latMu.Unlock()
			}
		})
	}
	wg.Wait()
}

func summarize(label, path string, completed, bad, failed int64, lat []int64, duration time.Duration) {
	slices.Sort(lat)
	total := completed + bad + failed
	if total == 0 || len(lat) == 0 {
		fmt.Printf("%-10s %-24s no requests completed\n", label, path)
		return
	}
	var totalNs int64
	for _, v := range lat {
		totalNs += v
	}
	pct := func(num, den int) float64 {
		idx := len(lat) * num / den
		if idx >= len(lat) {
			idx = len(lat) - 1
		}
		return float64(lat[idx]) / float64(time.Millisecond)
	}
	avgMs := float64(totalNs) / float64(len(lat)) / float64(time.Millisecond)
	rps := float64(completed) / duration.Seconds()
	fmt.Printf("%-10s %-24s ok=%-7d bad=%-7d failed=%-5d rps=%-7.0f avg=%6.2fms p50=%6.2fms p95=%6.2fms p99=%6.2fms p99.9=%6.2fms max=%7.2fms\n",
		label, path, completed, bad, failed, rps, avgMs,
		pct(50, 100), pct(95, 100), pct(99, 100), pct(999, 1000),
		float64(lat[len(lat)-1])/float64(time.Millisecond))
}

// startProgress 每 30s 打印一次累计与区间吞吐，用于长时压测观察趋势；返回停止函数
func startProgress(stats []epStat) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		var n int64
		var prevOK int64
		for {
			select {
			case <-done:
				return
			case <-t.C:
				n += 30
				var ok, bad, failed int64
				for i := range stats {
					ok += stats[i].completed.Load()
					bad += stats[i].bad.Load()
					failed += stats[i].failed.Load()
				}
				fmt.Printf("progress t=%ds ok=%d(+%.0f/s) bad=%d failed=%d\n",
					n, ok, float64(ok-prevOK)/30, bad, failed)
				prevOK = ok
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func main() {
	base := flag.String("base", "http://127.0.0.1:10001", "server base URL")
	epSpec := flag.String("endpoints", "all", "endpoints: all, or comma list of login,version,validate")
	concurrency := flag.Int("c", 100, "concurrent connections")
	duration := flag.Duration("d", 10*time.Second, "test duration")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration")
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())
	baseURL := strings.TrimRight(*base, "/")

	eps, err := selectEndpoints(*epSpec)
	if err != nil {
		fmt.Println(err)
		return
	}
	names := make([]string, len(eps))
	for i := range eps {
		names[i] = eps[i].name
	}

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

	fmt.Printf("base=%s endpoints=%s concurrency=%d duration=%s warmup=%s\n",
		baseURL, strings.Join(names, ","), *concurrency, *duration, *warmup)
	primeValidate(client, baseURL, eps)

	if *warmup > 0 {
		fmt.Printf("warming up for %s...\n", *warmup)
		runPhase(client, baseURL, eps, *concurrency, time.Now().Add(*warmup), nil, nil, nil)
	}
	runtime.GC()

	stats := make([]epStat, len(eps))
	lat := make([][]int64, len(eps))
	var latMu sync.Mutex
	stopProgress := startProgress(stats)
	runPhase(client, baseURL, eps, *concurrency, time.Now().Add(*duration), stats, &latMu, lat)
	stopProgress()
	transport.CloseIdleConnections()

	var all []int64
	var sumCompleted, sumBad, sumFailed int64
	for i := range eps {
		all = append(all, lat[i]...)
		sumCompleted += stats[i].completed.Load()
		sumBad += stats[i].bad.Load()
		sumFailed += stats[i].failed.Load()
	}
	fmt.Println()
	summarize("[overall]", "all endpoints", sumCompleted, sumBad, sumFailed, all, *duration)
	for i := range eps {
		summarize("["+eps[i].name+"]", eps[i].path,
			stats[i].completed.Load(), stats[i].bad.Load(), stats[i].failed.Load(), lat[i], *duration)
	}
}
