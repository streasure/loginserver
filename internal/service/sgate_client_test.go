package service

import "testing"

// TestNewSgateClientPoolNilDiscovery 未配置发现时池可用但无 source（AllIDs 为空、Destroy 安全）
// （注册地址解析表驱动测试见 util/netutil/register_test.go）
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
