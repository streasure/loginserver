package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/streasure/loginserver/internal/dmodel"
	"github.com/streasure/loginserver/internal/rmodel"
)

// fakeAclCache 内存版 aclCache，模拟共享 redis（多协程/多节点共用一份）
type fakeAclCache struct {
	mu      sync.Mutex
	gen     int64
	entries map[string]*rmodel.AclEntry

	getGenErr   error
	getEntryErr error
	getGenCalls atomic.Int32
}

func newFakeAclCache() *fakeAclCache {
	return &fakeAclCache{entries: make(map[string]*rmodel.AclEntry)}
}

func fakeKey(kind, id string) string { return kind + ":" + id }

func (f *fakeAclCache) GetGen(context.Context) (int64, error) {
	f.getGenCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gen, f.getGenErr
}

func (f *fakeAclCache) IncrGen(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen++
	return f.gen, nil
}

func (f *fakeAclCache) GetEntry(_ context.Context, kind, id string) (*rmodel.AclEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getEntryErr != nil {
		return nil, f.getEntryErr
	}
	return f.entries[fakeKey(kind, id)], nil
}

func (f *fakeAclCache) SetEntry(_ context.Context, kind, id string, entry *rmodel.AclEntry, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[fakeKey(kind, id)] = entry
	return nil
}

func (f *fakeAclCache) bumpTo(gen int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen = gen
}

// fakeAclStore 内存版名单数据源（模拟 MySQL；多节点共享同一份）
type fakeAclStore struct {
	mu     sync.Mutex
	white  map[string]struct{}
	black  map[string]struct{}
	roles  map[string]struct{}
	ipsErr error
}

func newFakeAclStore() *fakeAclStore {
	return &fakeAclStore{
		white: make(map[string]struct{}),
		black: make(map[string]struct{}),
		roles: make(map[string]struct{}),
	}
}

func (f *fakeAclStore) loaders() aclLoaders {
	setToSlice := func(m map[string]struct{}) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		return out
	}
	return aclLoaders{
		ips: func(context.Context) ([]string, []string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.ipsErr != nil {
				return nil, nil, f.ipsErr
			}
			return setToSlice(f.white), setToSlice(f.black), nil
		},
		roles: func(context.Context) ([]string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return setToSlice(f.roles), nil
		},
	}
}

func newTestAclService(cache aclCache, store *fakeAclStore) *AclService {
	return &AclService{
		cache:      cache,
		loaders:    store.loaders(),
		ttl:        time.Minute,
		genRefresh: time.Hour, // 测试中不主动轮询（用例显式控制）
	}
}

// loadTestSnapshot 模拟组件 Init：全量加载快照
func loadTestSnapshot(t *testing.T, svc *AclService) {
	t.Helper()
	if err := svc.Init(); err != nil {
		t.Fatalf("snapshot init failed: %v", err)
	}
}

// 读路径语义：白名单放行（跳过黑名单），黑名单拦截，封禁角色拦截
func TestAclCheckSemantics(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	store.white["1.1.1.1"] = struct{}{}
	store.black["1.1.1.1"] = struct{}{}
	store.black["2.2.2.2"] = struct{}{}
	store.roles["bad-role"] = struct{}{}
	svc := newTestAclService(cache, store)
	loadTestSnapshot(t, svc)

	if err := svc.CheckLoginIP(ctx, "1.1.1.1"); err != nil {
		t.Errorf("whitelist should bypass blacklist, got %v", err)
	}
	if err := svc.CheckLoginIP(ctx, "2.2.2.2"); !errors.Is(err, ErrIPBlacklisted) {
		t.Errorf("blacklist ip: got %v, want ErrIPBlacklisted", err)
	}
	if err := svc.CheckLoginIP(ctx, "3.3.3.3"); err != nil {
		t.Errorf("normal ip should pass, got %v", err)
	}
	if err := svc.CheckRole(ctx, "bad-role"); !errors.Is(err, ErrRoleBanned) {
		t.Errorf("banned role: got %v, want ErrRoleBanned", err)
	}
	if err := svc.CheckRole(ctx, "good-role"); err != nil {
		t.Errorf("normal role should pass, got %v", err)
	}
}

// 快照加载后读路径零存储依赖：redis 全故障不影响判定
func TestAclSnapshotReadZeroStorageDependency(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	store.black["9.9.9.9"] = struct{}{}
	svc := newTestAclService(cache, store)
	loadTestSnapshot(t, svc)

	// redis 完全故障
	cache.getGenErr = errors.New("redis down")
	cache.getEntryErr = errors.New("redis down")

	if err := svc.CheckLoginIP(ctx, "9.9.9.9"); !errors.Is(err, ErrIPBlacklisted) {
		t.Errorf("blacklist should work with redis down, got %v", err)
	}
	if err := svc.CheckLoginIP(ctx, "8.8.8.8"); err != nil {
		t.Errorf("pass should work with redis down, got %v", err)
	}
}

// 写路径读己之写：落库后本地快照即时生效（无需等重载）
func TestAclWriteApplyLocalImmediate(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	svc := newTestAclService(cache, store)
	loadTestSnapshot(t, svc)

	// 注入写路径数据源（生产为 dmodel 落库；测试同步 store 模拟落库成功）
	svc.writeIP = func(ctx context.Context, kind, ip, remark, operator string, add bool) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		if add {
			if kind == rmodel.AclKindWhiteIP {
				store.white[ip] = struct{}{}
			} else {
				store.black[ip] = struct{}{}
			}
			return nil
		}
		if kind == rmodel.AclKindWhiteIP {
			delete(store.white, ip)
		} else {
			delete(store.black, ip)
		}
		return nil
	}
	svc.writeRole = func(ctx context.Context, roleID, reason, operator string, add bool) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		if add {
			store.roles[roleID] = struct{}{}
		} else {
			delete(store.roles, roleID)
		}
		return nil
	}

	if err := svc.AddBlackIP(ctx, "5.5.5.5", "r", "op"); err != nil {
		t.Fatal(err)
	}
	if hit, err := svc.IsBlacklistedIP(ctx, "5.5.5.5"); err != nil || !hit {
		t.Errorf("write-your-reading: hit=%v err=%v", hit, err)
	}
	if got := svc.gen.Load(); got != 1 {
		t.Errorf("gen after write = %d, want 1", got)
	}

	if err := svc.RemoveBlackIP(ctx, "5.5.5.5", "op"); err != nil {
		t.Fatal(err)
	}
	if hit, err := svc.IsBlacklistedIP(ctx, "5.5.5.5"); err != nil || hit {
		t.Errorf("remove-your-reading: hit=%v err=%v", hit, err)
	}

	if err := svc.AddBannedRole(ctx, "acc-1", "reason", "op"); err != nil {
		t.Fatal(err)
	}
	if hit, err := svc.IsRoleBanned(ctx, "acc-1"); err != nil || !hit {
		t.Errorf("role ban write-your-reading: hit=%v err=%v", hit, err)
	}
	if err := svc.RemoveBannedRole(ctx, "acc-1", "op"); err != nil {
		t.Fatal(err)
	}
	if hit, err := svc.IsRoleBanned(ctx, "acc-1"); err != nil || hit {
		t.Errorf("role unban write-your-reading: hit=%v err=%v", hit, err)
	}
}

// 其他节点 gen 广播 → pollGen 发现差异 → 全量重载可见新数据
func TestAclRemoteWriteVisibleAfterPoll(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	// 节点 A/B 共享 redis 与数据源
	nodeA := newTestAclService(cache, store)
	nodeB := newTestAclService(cache, store)
	loadTestSnapshot(t, nodeA)
	loadTestSnapshot(t, nodeB)

	// 节点 A 写入（模拟：数据源更新 + INCR 广播）
	store.mu.Lock()
	store.black["7.7.7.7"] = struct{}{}
	store.mu.Unlock()
	cache.IncrGen(ctx)

	// 节点 B 轮询：gen 差异 → 重载 → 可见
	nodeB.pollGen()
	if hit, err := nodeB.IsBlacklistedIP(ctx, "7.7.7.7"); err != nil || !hit {
		t.Errorf("remote write should be visible after poll, hit=%v err=%v", hit, err)
	}
}

// 重载失败（MySQL 故障）保留旧快照继续服务
func TestAclReloadFailureKeepsOldSnapshot(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	store.black["1.2.3.4"] = struct{}{}
	svc := newTestAclService(cache, store)
	loadTestSnapshot(t, svc)

	store.mu.Lock()
	store.ipsErr = errors.New("mysql down")
	store.mu.Unlock()

	if err := svc.reload(ctx); err == nil {
		t.Fatal("reload should fail with mysql down")
	}
	// 旧快照仍可服务
	if hit, err := svc.IsBlacklistedIP(ctx, "1.2.3.4"); err != nil || !hit {
		t.Errorf("old snapshot should keep serving, hit=%v err=%v", hit, err)
	}
}

// 未初始化时首次读同步加载（组件 Init 前兜底）
func TestAclSnapshotLazyLoad(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	store.roles["lazy-role"] = struct{}{}
	svc := newTestAclService(cache, store)

	if hit, err := svc.IsRoleBanned(ctx, "lazy-role"); err != nil || !hit {
		t.Fatalf("lazy load: hit=%v err=%v", hit, err)
	}
}

// IP 规范化：IPv6 压缩形式与入库口径一致
func TestAclIPNormalization(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	store := newFakeAclStore()
	svc := newTestAclService(cache, store)
	loadTestSnapshot(t, svc)

	svc.writeIP = func(context.Context, string, string, string, string, bool) error { return nil }
	// AddBlackIP 内部会 normalize；store 需要已含规范化键
	store.mu.Lock()
	store.black["2001:db8::1"] = struct{}{}
	store.mu.Unlock()
	if err := svc.reload(ctx); err != nil {
		t.Fatal(err)
	}
	if hit, err := svc.IsBlacklistedIP(ctx, "2001:0db8:0000:0000:0000:0000:0000:0001"); err != nil || !hit {
		t.Errorf("compressed ipv6 should match normalized form, hit=%v err=%v", hit, err)
	}
	// 非法 IP 不在名单也不报错
	if hit, err := svc.IsBlacklistedIP(ctx, "not-an-ip"); err != nil || hit {
		t.Errorf("invalid ip: hit=%v err=%v, want false/nil", hit, err)
	}
}

// 服务器列表缓存往返（JSON 负载 + 版本校验）
func TestAclServerListCacheRoundtrip(t *testing.T) {
	cache := newFakeAclCache()
	store := newFakeAclStore()
	svc := newTestAclService(cache, store)

	want := []dmodel.ServerInfo{{ServerID: "s1", ServerName: "server-1", Port: 10001, Status: 1, Sort: 1}}
	svc.storeServers(context.Background(), want)

	got, ok := svc.lookupServers(context.Background())
	if !ok {
		t.Fatal("server list should hit cache")
	}
	if len(got) != 1 || got[0].ServerID != "s1" || got[0].Port != 10001 {
		t.Errorf("got = %+v", got)
	}

	// 版本过期 → 拒绝
	svc.genAt.Store(0)
	svc.genRefresh = time.Nanosecond
	cache.bumpTo(99)
	if _, ok := svc.lookupServers(context.Background()); ok {
		t.Error("stale server list should be rejected")
	}
}
