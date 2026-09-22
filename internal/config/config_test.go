package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 基础配置：满足 validate 必填项，Redis 连接池参数由各用例追加
const baseYaml = `
belong: "default"
serverType: "LoginServer"
zone: "default"
serverId: "loginserver-1"

ports:
  httpAddr: 10001
  grpcServiceAddr: 10002
  pprofPort: 10003

redis:
  login:
    network: tcp
    address: "127.0.0.1:6379"
    db: 1

etcd:
  endpoints: ["http://127.0.0.1:2379"]
  servicePrefix: "/services"

limits:
  loginTokenExpireSeconds: 86400
  validateTokenCacheTtl: "1s"

readTimeoutSec: 3
writeTimeoutSec: 3
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loginserver.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// 显式配置的连接池参数应原样解析，duration 字符串应可被 ParseDuration 解析
func TestLoadRedisPoolConfigExplicit(t *testing.T) {
	yaml := baseYaml + `
RedisMinIdleConns: 2
RedisMaxIdleConns: 40
RedisMaxOpenConns: 40
RedisMaxIdleTime: 300s
RedisMaxLifeTime: 3600s
`
	if err := LoadConfig(writeTempConfig(t, yaml)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := GetConfig()
	if cfg.RedisMinIdleConns != 2 {
		t.Errorf("RedisMinIdleConns = %d, want 2", cfg.RedisMinIdleConns)
	}
	if cfg.RedisMaxIdleConns != 40 {
		t.Errorf("RedisMaxIdleConns = %d, want 40", cfg.RedisMaxIdleConns)
	}
	if cfg.RedisMaxOpenConns != 40 {
		t.Errorf("RedisMaxOpenConns = %d, want 40", cfg.RedisMaxOpenConns)
	}
	for _, d := range []struct{ name, val string }{
		{"RedisMaxIdleTime", cfg.RedisMaxIdleTime},
		{"RedisMaxLifeTime", cfg.RedisMaxLifeTime},
	} {
		parsed, err := time.ParseDuration(d.val)
		if err != nil {
			t.Fatalf("%s = %q, ParseDuration error: %v", d.name, d.val, err)
		}
		if parsed <= 0 {
			t.Errorf("%s = %q, want positive duration", d.name, d.val)
		}
	}
}

// 未配置时字段为零值（uconfig 不支持 default tag），由组件构造侧按"0/空 = uredis 默认"兜底
func TestLoadRedisPoolConfigDefault(t *testing.T) {
	if err := LoadConfig(writeTempConfig(t, baseYaml)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := GetConfig()
	if cfg.RedisMinIdleConns != 0 || cfg.RedisMaxIdleConns != 0 || cfg.RedisMaxOpenConns != 0 {
		t.Errorf("pool conns should be zero, got min=%d maxIdle=%d maxOpen=%d",
			cfg.RedisMinIdleConns, cfg.RedisMaxIdleConns, cfg.RedisMaxOpenConns)
	}
	if cfg.RedisMaxIdleTime != "" || cfg.RedisMaxLifeTime != "" {
		t.Errorf("pool durations should be empty, got idle=%q life=%q",
			cfg.RedisMaxIdleTime, cfg.RedisMaxLifeTime)
	}
}

// duration 格式非法时启动应报错（validate 早失败）
func TestLoadRedisPoolConfigInvalidDuration(t *testing.T) {
	cases := []string{"RedisMaxIdleTime: abc\n", "RedisMaxLifeTime: 100\n"} // 100 无单位，ParseDuration 失败
	for _, extra := range cases {
		if err := LoadConfig(writeTempConfig(t, baseYaml+extra)); err == nil {
			t.Errorf("LoadConfig with %q should fail, got nil", extra)
		}
	}
}
