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

	getGenErr    error
	getEntryErr  error
	getEntryHits atomic.Int32
}

func newFakeAclCache() *fakeAclCache {
	return &fakeAclCache{entries: make(map[string]*rmodel.AclEntry)}
}

func fakeKey(kind, id string) string { return kind + ":" + id }

func (f *fakeAclCache) GetGen(context.Context) (int64, error) {
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
	f.getEntryHits.Add(1)
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

func (f *fakeAclCache) setEntryRaw(kind, id string, entry *rmodel.AclEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[fakeKey(kind, id)] = entry
}

func newTestAclService(cache aclCache) *AclService {
	return &AclService{
		cache:      cache,
		ttl:        time.Minute,
		genRefresh: time.Hour, // 测试中不主动过期快照（用例显式控制）
	}
}

// 首次回源后命中缓存，负缓存同样生效（loader 只调用一次）
func TestAclCheckCachedCachesResult(t *testing.T) {
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

	var loads atomic.Int32
	load := func(context.Context) (bool, error) {
		loads.Add(1)
		return false, nil
	}

	for range 3 {
		hit, err := svc.checkCached(context.Background(), rmodel.AclKindBlackIP, "1.2.3.4", load)
		if err != nil || hit {
			t.Fatalf("hit=%v err=%v, want false/nil", hit, err)
		}
	}
	if loads.Load() != 1 {
		t.Errorf("loads = %d, want 1 (singleflight + cache)", loads.Load())
	}

	// 负缓存：即使 loader 返回 true，也应命中缓存的 false，loader 不被调用
	hit, err := svc.checkCached(context.Background(), rmodel.AclKindBlackIP, "1.2.3.4",
		func(context.Context) (bool, error) {
			loads.Add(1)
			return true, nil
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hit {
		t.Error("negative cache should serve false")
	}
	if loads.Load() != 1 {
		t.Errorf("loads = %d, want 1 (negative cache hit)", loads.Load())
	}

	// 正向缓存：另一键回源 true 后，换 loader 也不被调用
	if _, err := svc.checkCached(context.Background(), rmodel.AclKindWhiteIP, "5.6.7.8",
		func(context.Context) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	hit, err = svc.checkCached(context.Background(), rmodel.AclKindWhiteIP, "5.6.7.8",
		func(context.Context) (bool, error) {
			loads.Add(1)
			return false, nil
		})
	if err != nil || !hit {
		t.Fatalf("hit=%v err=%v, want true from cache", hit, err)
	}
	if loads.Load() != 1 {
		t.Errorf("loads = %d, want 1 (positive cache hit)", loads.Load())
	}
}

// publish 后：其他节点本地版本快照虽旧，也因条目版本更高立即生效（无需等 genRefresh）
func TestAclPublishNewerEntryVisibleWithoutGenRefresh(t *testing.T) {
	cache := newFakeAclCache()
	nodeA := newTestAclService(cache)
	nodeB := newTestAclService(cache)

	// 节点 B 先建立旧快照（gen=0）
	nodeB.lookup(context.Background(), rmodel.AclKindBlackIP, "9.9.9.9")

	// 节点 A 写入：INCR gen=1，条目 ver=1
	nodeA.publish(context.Background(), rmodel.AclKindBlackIP, "9.9.9.9", true)

	// 节点 B 本地快照仍是 0，但条目 ver=1 >= 0 → 立即可见
	hit, ok := nodeB.lookup(context.Background(), rmodel.AclKindBlackIP, "9.9.9.9")
	if !ok || !hit {
		t.Errorf("entry should be visible immediately, hit=%v ok=%v", hit, ok)
	}
}

// publish 后写入方本地版本快照立即同步（读己之写，无需等 genRefresh）
func TestAclPublishSyncsLocalGen(t *testing.T) {
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

	svc.publish(context.Background(), rmodel.AclKindBlackIP, "1.1.1.1", true)
	if got := svc.gen.Load(); got != 1 {
		t.Errorf("local gen after publish = %d, want 1 (synced)", got)
	}
	svc.publish(context.Background(), rmodel.AclKindRole, "acc-1", true)
	if got := svc.gen.Load(); got != 2 {
		t.Errorf("local gen after second publish = %d, want 2 (synced)", got)
	}
}

// 本地版本快照刷新后，版本旧于全局版本的条目被判过期回源（跨节点删除的收敛路径）
func TestAclStaleEntryRejectedAfterGenRefresh(t *testing.T) {
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

	// 全局 gen 已被推到 2（其他节点写过两次），本地条目 ver=0
	cache.bumpTo(2)
	cache.setEntryRaw(rmodel.AclKindRole, "acc-1", &rmodel.AclEntry{Ver: 0, Hit: true})

	svc.genRefresh = 0 // 快照立即过期：读前强制刷新 gen
	hit, ok := svc.lookup(context.Background(), rmodel.AclKindRole, "acc-1")
	if ok {
		t.Errorf("stale entry should be rejected, got hit=%v", hit)
	}

	// 条目版本追平全局版本后接受
	cache.setEntryRaw(rmodel.AclKindRole, "acc-1", &rmodel.AclEntry{Ver: 2, Hit: true})
	hit, ok = svc.lookup(context.Background(), rmodel.AclKindRole, "acc-1")
	if !ok || !hit {
		t.Errorf("fresh entry should be accepted, hit=%v ok=%v", hit, ok)
	}
}

// redis 整体不可用时绕过缓存直查数据源（登录不因缓存故障失败）
func TestAclDegradeToLoaderOnCacheError(t *testing.T) {
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

	var loads atomic.Int32
	load := func(context.Context) (bool, error) {
		loads.Add(1)
		return true, nil
	}

	hit, err := svc.checkCached(context.Background(), rmodel.AclKindWhiteIP, "10.0.0.1", load)
	if err != nil || !hit {
		t.Fatalf("hit=%v err=%v", hit, err)
	}

	// redis 完全故障：应降级回源成功
	cache.getEntryErr = errors.New("redis down")
	cache.getGenErr = errors.New("redis down")
	svc.genAt.Store(0)

	before := loads.Load()
	hit, err = svc.checkCached(context.Background(), rmodel.AclKindWhiteIP, "10.0.0.1", load)
	if err != nil || !hit {
		t.Fatalf("degraded hit=%v err=%v, want loader result", hit, err)
	}
	if loads.Load() != before+1 {
		t.Errorf("loads delta = %d, want 1 (bypass cache)", loads.Load()-before)
	}
}

// 并发同键 miss 只回源一次（singleflight）
func TestAclSingleflightDedup(t *testing.T) {
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

	var loads atomic.Int32
	load := func(context.Context) (bool, error) {
		loads.Add(1)
		time.Sleep(50 * time.Millisecond)
		return true, nil
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			hit, err := svc.checkCached(context.Background(), rmodel.AclKindBlackIP, "8.8.8.8", load)
			if err != nil || !hit {
				t.Errorf("hit=%v err=%v", hit, err)
			}
		})
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Errorf("loads = %d, want 1", loads.Load())
	}
}

// 服务器列表缓存往返（JSON 负载 + 版本校验）
func TestAclServerListCacheRoundtrip(t *testing.T) {
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

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
	svc.genRefresh = 0
	cache.bumpTo(99)
	if _, ok := svc.lookupServers(context.Background()); ok {
		t.Error("stale server list should be rejected")
	}
}

// 白名单/黑名单/封禁判定语义：白名单放行，黑名单拦截，封禁角色拦截
func TestAclCheckSemantics(t *testing.T) {
	ctx := context.Background()
	cache := newFakeAclCache()
	svc := newTestAclService(cache)

	whiteSet := map[string]bool{"1.1.1.1": true}
	blackSet := map[string]bool{"1.1.1.1": true, "2.2.2.2": true}
	banned := map[string]bool{"bad-role": true}

	checkIP := func(ip string) error {
		white, err := svc.checkCached(ctx, rmodel.AclKindWhiteIP, ip, func(context.Context) (bool, error) {
			return whiteSet[ip], nil
		})
		if err != nil {
			return err
		}
		if white {
			return nil
		}
		black, err := svc.checkCached(ctx, rmodel.AclKindBlackIP, ip, func(context.Context) (bool, error) {
			return blackSet[ip], nil
		})
		if err != nil {
			return err
		}
		if black {
			return ErrIPBlacklisted
		}
		return nil
	}
	checkRole := func(id string) error {
		hit, err := svc.checkCached(ctx, rmodel.AclKindRole, id, func(context.Context) (bool, error) {
			return banned[id], nil
		})
		if err != nil {
			return err
		}
		if hit {
			return ErrRoleBanned
		}
		return nil
	}

	if err := checkIP("1.1.1.1"); err != nil {
		t.Errorf("whitelist should bypass blacklist, got %v", err)
	}
	if err := checkIP("2.2.2.2"); !errors.Is(err, ErrIPBlacklisted) {
		t.Errorf("blacklist ip: got %v, want ErrIPBlacklisted", err)
	}
	if err := checkIP("3.3.3.3"); err != nil {
		t.Errorf("normal ip should pass, got %v", err)
	}
	if err := checkRole("bad-role"); !errors.Is(err, ErrRoleBanned) {
		t.Errorf("banned role: got %v, want ErrRoleBanned", err)
	}
	if err := checkRole("good-role"); err != nil {
		t.Errorf("normal role should pass, got %v", err)
	}
}
