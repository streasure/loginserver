package service

import "testing"

// TestParseSgateGRPCAddr JSON 注册地址取 gRPC 端口；非 JSON 原样通过；残缺/空地址不可用
func TestParseSgateGRPCAddr(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"json full", `{"ip":"10.0.0.5","grpc":50051,"tcp":8080,"websocket":8081}`, "10.0.0.5:50051", true},
		{"plain address", "10.0.0.5:50051", "10.0.0.5:50051", true},
		{"json missing grpc", `{"ip":"10.0.0.5"}`, "", false},
		{"json empty", `{}`, "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseSgateGRPCAddr(c.raw)
			if got != c.want || ok != c.ok {
				t.Errorf("parseSgateGRPCAddr(%q) = (%q,%v), want (%q,%v)", c.raw, got, ok, c.want, c.ok)
			}
		})
	}
}

// TestNewSgateClientPoolNilDiscovery 未配置发现时池可用但无 source（AllIDs 为空、Destroy 安全）
func TestNewSgateClientPoolNilDiscovery(t *testing.T) {
	defer func() {
		sgatePool = nil
		sgatePoolInit.Store(false)
	}()
	p := NewSgateClientPool(nil, "test-token")
	if p == nil {
		t.Fatal("NewSgateClientPool(nil) should return pool")
	}
	if p.source != nil {
		t.Error("source should be nil without discovery")
	}
	if got := p.AllIDs(); got != nil {
		t.Errorf("AllIDs = %v, want nil", got)
	}
	p.Destroy()
}
