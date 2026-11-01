package service

import (
	"context"
	"strings"
	"testing"
)

// TestSgateKickWithoutRegistration 未注册任何 sgate 时（nil 句柄）踢人链路安全短路：
// 实例列表为空、按 ID 取 Kick 客户端返回未注册错误。注册/发现/建连路径见
// util/udiscovery 的单元与 etcd 集成测试。
func TestSgateKickWithoutRegistration(t *testing.T) {
	if ids := sgateKick.IDs(); len(ids) != 0 {
		t.Fatalf("sgateKick.IDs = %v, want empty", ids)
	}

	_, err := kickSgateIP(context.Background(), "sgate-1", "10.0.0.1", "test")
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("kickSgateIP err = %v, want not registered", err)
	}

	_, err = kickSgateAccounts(context.Background(), "sgate-1", []string{"acct-1"}, "test")
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("kickSgateAccounts err = %v, want not registered", err)
	}
}
