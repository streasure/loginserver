package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/streasure/loginserver/internal/dmodel"
)

// fakeAclStore 内存版名单数据源（模拟 MySQL）
type fakeAclStore struct {
	mu    sync.Mutex
	white []dmodel.WhitelistIP
	black []dmodel.BlacklistIP
	roles []dmodel.BannedRole
}

func newFakeAclStore() *fakeAclStore {
	return &fakeAclStore{}
}

func (f *fakeAclStore) addWhite(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.white = append(f.white, dmodel.WhitelistIP{IP: ip})
}

func (f *fakeAclStore) addBlack(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.black = append(f.black, dmodel.BlacklistIP{IP: ip})
}

func (f *fakeAclStore) addRole(roleID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roles = append(f.roles, dmodel.BannedRole{RoleID: roleID})
}

func newTestAclService() *AclService {
	return &AclService{}
}

// loadTestData 加载测试数据到快照（模拟 Init 加载 MySQL）
func loadTestData(svc *AclService, store *fakeAclStore) {
	store.mu.Lock()
	defer store.mu.Unlock()
	d := newAclData()
	for _, row := range store.white {
		applyIPEdge(d.white, &d.whitePrefix, row.IP, true)
	}
	for _, row := range store.black {
		applyIPEdge(d.black, &d.blackPrefix, row.IP, true)
	}
	for _, row := range store.roles {
		d.roles[row.RoleID] = struct{}{}
	}
	svc.snap.Store(d)
	updateAclSnapshotGauges(d)
}

// 读路径语义：白名单放行（跳过黑名单），黑名单拦截，封禁角色拦截
func TestAclCheckSemantics(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	store.addWhite("1.1.1.1")
	store.addBlack("1.1.1.1")
	store.addBlack("2.2.2.2")
	store.addRole("bad-role")
	svc := newTestAclService()
	loadTestData(svc, store)

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

// 快照加载后读路径零存储依赖
func TestAclSnapshotReadZeroStorageDependency(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	store.addBlack("9.9.9.9")
	svc := newTestAclService()
	loadTestData(svc, store)

	if err := svc.CheckLoginIP(ctx, "9.9.9.9"); !errors.Is(err, ErrIPBlacklisted) {
		t.Errorf("blacklist should work, got %v", err)
	}
	if err := svc.CheckLoginIP(ctx, "8.8.8.8"); err != nil {
		t.Errorf("pass should work, got %v", err)
	}
}

// 写路径读己之写：落库后本地快照即时生效（无需等重载）
func TestAclWriteApplyImmediate(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	svc := newTestAclService()
	loadTestData(svc, store)

	svc.applyBlackIP("5.5.5.5", true)
	if hit, err := svc.IsBlacklistedIP(ctx, "5.5.5.5"); err != nil || !hit {
		t.Errorf("write-your-reading: hit=%v err=%v", hit, err)
	}

	svc.applyBlackIP("5.5.5.5", false)
	if hit, err := svc.IsBlacklistedIP(ctx, "5.5.5.5"); err != nil || hit {
		t.Errorf("remove-your-reading: hit=%v err=%v", hit, err)
	}

	svc.applyRole("acc-1", true)
	if hit, err := svc.IsRoleBanned(ctx, "acc-1"); err != nil || !hit {
		t.Errorf("role ban write-your-reading: hit=%v err=%v", hit, err)
	}
	svc.applyRole("acc-1", false)
	if hit, err := svc.IsRoleBanned(ctx, "acc-1"); err != nil || hit {
		t.Errorf("role unban write-your-reading: hit=%v err=%v", hit, err)
	}
}

// IP 规范化：IPv6 压缩形式与入库口径一致
func TestAclIPNormalization(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	svc := newTestAclService()
	store.addBlack("2001:db8::1")
	loadTestData(svc, store)

	if hit, err := svc.IsBlacklistedIP(ctx, "2001:0db8:0000:0000:0000:0000:0000:0001"); err != nil || !hit {
		t.Errorf("compressed ipv6 should match normalized form, hit=%v err=%v", hit, err)
	}
	// 非法 IP 不在名单也不报错
	if hit, err := svc.IsBlacklistedIP(ctx, "not-an-ip"); err != nil || hit {
		t.Errorf("invalid ip: hit=%v err=%v, want false/nil", hit, err)
	}
}

// CIDR 黑白名单：网段条目匹配段内所有 IP，不匹配段外
func TestAclCIDRBlacklistMatch(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	store.addBlack("10.0.0.0/8")
	store.addBlack("192.168.1.100")
	store.addWhite("10.1.2.3")
	svc := newTestAclService()
	loadTestData(svc, store)

	// 段内 IP → 命中黑名单
	if hit, err := svc.IsBlacklistedIP(ctx, "10.255.255.255"); err != nil || !hit {
		t.Errorf("10.255.255.255 in 10.0.0.0/8: hit=%v err=%v", hit, err)
	}
	// 段外 IP → 不命中
	if hit, err := svc.IsBlacklistedIP(ctx, "11.0.0.1"); err != nil || hit {
		t.Errorf("11.0.0.1 not in 10.0.0.0/8: hit=%v err=%v", hit, err)
	}
	// 纯 IP 精确条目仍生效
	if hit, err := svc.IsBlacklistedIP(ctx, "192.168.1.100"); err != nil || !hit {
		t.Errorf("exact ip should hit: hit=%v err=%v", hit, err)
	}
	// 段内精确白名单优先（CheckLoginIP 白名单放行）
	if err := svc.CheckLoginIP(ctx, "10.1.2.3"); err != nil {
		t.Errorf("whitelist inside blacklisted cidr should pass, got %v", err)
	}
	// 段内非白名单 IP 被拦
	if err := svc.CheckLoginIP(ctx, "10.9.9.9"); !errors.Is(err, ErrIPBlacklisted) {
		t.Errorf("ip inside blacklisted cidr: got %v, want ErrIPBlacklisted", err)
	}
}

// CIDR 白名单：网段放行
func TestAclCIDRWhitelistMatch(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	store.addWhite("172.16.0.0/12")
	store.addBlack("172.16.0.5")
	svc := newTestAclService()
	loadTestData(svc, store)

	// 段内 IP 白名单放行
	if err := svc.CheckLoginIP(ctx, "172.31.255.254"); err != nil {
		t.Errorf("ip in whitelisted cidr should pass, got %v", err)
	}
	// 白名单段内 + 命中黑名单精确条目 → 白名单优先放行
	if err := svc.CheckLoginIP(ctx, "172.16.0.5"); err != nil {
		t.Errorf("ip in white cidr with exact black should pass (white wins), got %v", err)
	}
}

// CIDR apply 增删：add/remove 后快照即时生效（读己之写）
func TestAclCIDRApply(t *testing.T) {
	ctx := context.Background()
	store := newFakeAclStore()
	svc := newTestAclService()
	loadTestData(svc, store)

	// add CIDR
	svc.applyBlackIP("203.0.113.0/24", true)
	if hit, err := svc.IsBlacklistedIP(ctx, "203.0.113.50"); err != nil || !hit {
		t.Errorf("after add cidr: hit=%v err=%v", hit, err)
	}
	// remove CIDR
	svc.applyBlackIP("203.0.113.0/24", false)
	if hit, err := svc.IsBlacklistedIP(ctx, "203.0.113.50"); err != nil || hit {
		t.Errorf("after remove cidr: hit=%v err=%v, want false/nil", hit, err)
	}
}

// 并发写不丢更新：多 goroutine 并发 apply 后，快照必须包含全部增量。
func TestAclConcurrentApplyNoLostUpdate(t *testing.T) {
	store := newFakeAclStore()
	svc := newTestAclService()
	loadTestData(svc, store)

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			roleID := fmt.Sprintf("concurrent-role-%02d", i)
			svc.applyRole(roleID, true)
		}(i)
	}
	wg.Wait()

	d := svc.snap.Load()
	if d == nil {
		t.Fatal("snapshot nil after concurrent writes")
	}
	if len(d.roles) != n {
		t.Errorf("snapshot roles = %d, want %d（并发写丢失更新）", len(d.roles), n)
	}
}
