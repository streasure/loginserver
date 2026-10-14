package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 配置头部（满足 validate 必填项）；redis 段由各用例组合（baseYaml 为默认组合）
const headYaml = `
belong: "default"
serverType: "LoginServer"
zone: "default"
serverId: "loginserver-1"

ports:
  httpAddr: 10001
  grpcServiceAddr: 10002
  pprofPort: 10003

etcd:
  endpoints: ["http://127.0.0.1:2379"]
  servicePrefix: "/services"

limits:
  loginTokenExpireSeconds: 86400
  validateTokenCacheTtl: "1s"

db:
  configs:
    logindb:
      dbname: "loginserver"
      host: "127.0.0.1"
      port: 3306
      uid: "root"
      pwd: "root"
  maxIdleConns: 10
  maxOpenConns: 50

acl:
  cacheTtl: 5m
  genRefresh: 2s
  adminToken: "test-admin-token"
`

// 默认 redis 段：实例 + 读写超时（连接池参数未配置 → 0/空 = uredis 默认值）
const redisYaml = `
redis:
  readTimeout: 3s
  writeTimeout: 3s
  instances:
    login:
      network: tcp
      address: "127.0.0.1:6379"
      db: 1
`

// baseYaml 满足 validate 必填项的完整配置
const baseYaml = headYaml + redisYaml

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loginserver.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// 显式配置的 redis 段应原样解析到 uredis.Config（实例连接信息 + 连接池/超时参数）
func TestLoadRedisPoolConfigExplicit(t *testing.T) {
	yaml := headYaml + `
redis:
  readTimeout: 5s
  writeTimeout: 6s
  minIdleConns: 2
  maxIdleConns: 40
  poolSize: 40
  maxIdleTime: 300s
  maxLifeTime: 3600s
  instances:
    login:
      network: tcp
      address: "127.0.0.1:6379"
      db: 1
`
	if err := LoadConfig(writeTempConfig(t, yaml)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := GetConfig()
	if cfg.Redis.MinIdleConns != 2 {
		t.Errorf("Redis.MinIdleConns = %d, want 2", cfg.Redis.MinIdleConns)
	}
	if cfg.Redis.MaxIdleConns != 40 {
		t.Errorf("Redis.MaxIdleConns = %d, want 40", cfg.Redis.MaxIdleConns)
	}
	if cfg.Redis.PoolSize != 40 {
		t.Errorf("Redis.PoolSize = %d, want 40", cfg.Redis.PoolSize)
	}
	if cfg.Redis.ReadTimeout != "5s" || cfg.Redis.WriteTimeout != "6s" {
		t.Errorf("Redis read/writeTimeout = %q/%q, want 5s/6s",
			cfg.Redis.ReadTimeout, cfg.Redis.WriteTimeout)
	}
	for _, d := range []struct{ name, val string }{
		{"MaxIdleTime", cfg.Redis.MaxIdleTime},
		{"MaxLifeTime", cfg.Redis.MaxLifeTime},
	} {
		parsed, err := time.ParseDuration(d.val)
		if err != nil {
			t.Fatalf("Redis.%s = %q, ParseDuration error: %v", d.name, d.val, err)
		}
		if parsed <= 0 {
			t.Errorf("Redis.%s = %q, want positive duration", d.name, d.val)
		}
	}
	inst, ok := cfg.Redis.Instances["login"]
	if !ok || inst.Address != "127.0.0.1:6379" || inst.DB != 1 {
		t.Errorf("Redis.Instances[login] = %+v, want address/db parsed", inst)
	}
}

// 未配置的连接池参数为零值（0/空 = uredis 默认，由 uredis.Config.toOptions 不覆盖）
func TestLoadRedisPoolConfigDefault(t *testing.T) {
	if err := LoadConfig(writeTempConfig(t, baseYaml)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := GetConfig()
	if cfg.Redis.MinIdleConns != 0 || cfg.Redis.MaxIdleConns != 0 || cfg.Redis.PoolSize != 0 {
		t.Errorf("pool conns should be zero, got min=%d maxIdle=%d poolSize=%d",
			cfg.Redis.MinIdleConns, cfg.Redis.MaxIdleConns, cfg.Redis.PoolSize)
	}
	if cfg.Redis.MaxIdleTime != "" || cfg.Redis.MaxLifeTime != "" {
		t.Errorf("pool durations should be empty, got idle=%q life=%q",
			cfg.Redis.MaxIdleTime, cfg.Redis.MaxLifeTime)
	}
	if cfg.Redis.ReadTimeout != "3s" || cfg.Redis.WriteTimeout != "3s" {
		t.Errorf("Redis read/writeTimeout = %q/%q, want 3s/3s",
			cfg.Redis.ReadTimeout, cfg.Redis.WriteTimeout)
	}
}

// Etcd/Perf 配置类型已抽象到 util：uetcd.EtcdInfo / uperf.Config。
// yaml 未配置的项按类型的 default 标签填充（leaseTTL 10s、gcPercent 100、memoryLimitPercent 90）
func TestLoadEtcdPerfDefaults(t *testing.T) {
	if err := LoadConfig(writeTempConfig(t, baseYaml)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := GetConfig()
	if len(cfg.Etcd.Endpoints) != 1 || cfg.Etcd.Endpoints[0] != "http://127.0.0.1:2379" {
		t.Errorf("Etcd.Endpoints = %v, want [http://127.0.0.1:2379]", cfg.Etcd.Endpoints)
	}
	if cfg.Etcd.ServicePrefix != "/services" {
		t.Errorf("Etcd.ServicePrefix = %q, want /services", cfg.Etcd.ServicePrefix)
	}
	if cfg.Etcd.LeaseTTL != "10s" {
		t.Errorf("Etcd.LeaseTTL = %q, want 10s (uetcd.EtcdInfo default)", cfg.Etcd.LeaseTTL)
	}
	if cfg.Perf.GcPercent != 100 {
		t.Errorf("Perf.GcPercent = %d, want 100 (uperf.Config default)", cfg.Perf.GcPercent)
	}
	if cfg.Perf.MemoryLimitPercent != 90 {
		t.Errorf("Perf.MemoryLimitPercent = %d, want 90 (uperf.Config default)", cfg.Perf.MemoryLimitPercent)
	}
}

// 显式配置的 perf/etcd 项不能被 default 覆盖（uconfig 键查找须与 mapstructure 大小写一致）
func TestLoadEtcdPerfExplicit(t *testing.T) {
	yaml := strings.Replace(baseYaml, `  servicePrefix: "/services"`, `  servicePrefix: "/services"
  leaseTTL: "30s"`, 1) + `
perf:
  gcPercent: 300
  memoryLimitPercent: 85
`
	if err := LoadConfig(writeTempConfig(t, yaml)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := GetConfig()
	if cfg.Perf.GcPercent != 300 {
		t.Errorf("Perf.GcPercent = %d, want explicit 300", cfg.Perf.GcPercent)
	}
	if cfg.Perf.MemoryLimitPercent != 85 {
		t.Errorf("Perf.MemoryLimitPercent = %d, want explicit 85", cfg.Perf.MemoryLimitPercent)
	}
	if cfg.Etcd.LeaseTTL != "30s" {
		t.Errorf("Etcd.LeaseTTL = %q, want explicit 30s", cfg.Etcd.LeaseTTL)
	}
}

// redis 配置非法时启动应报错（validate 下沉到 uredis.Config.Validate，早失败）
func TestLoadRedisConfigInvalid(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "maxIdleTime without format",
			yaml: headYaml + `
redis:
  maxIdleTime: abc
  instances:
    login:
      address: "127.0.0.1:6379"
`,
		},
		{
			name: "maxLifeTime without unit",
			yaml: headYaml + `
redis:
  maxLifeTime: 100
  instances:
    login:
      address: "127.0.0.1:6379"
`,
		},
		{
			name: "missing instances",
			yaml: headYaml,
		},
		{
			name: "instance missing address",
			yaml: headYaml + `
redis:
  instances:
    login:
      db: 1
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := LoadConfig(writeTempConfig(t, tc.yaml)); err == nil {
				t.Error("LoadConfig should fail, got nil")
			}
		})
	}
}
