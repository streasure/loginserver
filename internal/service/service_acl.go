package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/dmodel"
	"github.com/streasure/loginserver/internal/rmodel"
	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"golang.org/x/sync/singleflight"
)

// ErrIPBlacklisted 登录 IP 命中黑名单
var ErrIPBlacklisted = errors.New("ip blacklisted")

// ErrRoleBanned 角色已被封禁
var ErrRoleBanned = errors.New("role banned")

// aclData 全量 ACL 内存快照（白名单/黑名单/封禁角色）。
// IP 条目分两类：纯 IP 进 exact 集合（O(1) 查）；CIDR 进前缀切片（线性扫，
// 条目量小——生产通常 <100——且前缀 Contains 为纯位运算，足够快）。
type aclData struct {
	white       map[string]struct{}
	black       map[string]struct{}
	whitePrefix []netip.Prefix
	blackPrefix []netip.Prefix
	roles       map[string]struct{}
}

func newAclData() *aclData {
	return &aclData{
		white: make(map[string]struct{}),
		black: make(map[string]struct{}),
		roles: make(map[string]struct{}),
	}
}

func (d *aclData) clone() *aclData {
	n := newAclData()
	for k := range d.white {
		n.white[k] = struct{}{}
	}
	for k := range d.black {
		n.black[k] = struct{}{}
	}
	n.whitePrefix = append(n.whitePrefix[:0], d.whitePrefix...)
	n.blackPrefix = append(n.blackPrefix[:0], d.blackPrefix...)
	for k := range d.roles {
		n.roles[k] = struct{}{}
	}
	return n
}

// AclService 黑白名单/封禁角色/服务器列表服务。
//
// 极简架构（三层缓存：本地全量快照 → Redis per-key → MySQL）：
//   - 启动时从 MySQL 全量加载快照（白/黑名单/封禁角色）
//   - 读路径：快照查询（零存储依赖，O(1) exact + O(n) CIDR 前缀扫描）
//   - 写路径：MySQL 落库 → 本地快照即时应用（mu 串行化）→ 删除 Redis 缓存
//   - 多节点同步：依赖 MySQL 权威源 + 节点重启重新加载（写少读多，可接受短暂不同步）
//   - 删除机制：gen 版本广播、后台轮询、Redis 全量快照缓存
type AclService struct {
	component.BaseComponent
	snap     atomic.Pointer[aclData] // 全量快照（写路径原子替换）
	mu       sync.Mutex              // 串行化快照写路径（applyLocal）
	sf       singleflight.Group      // servers 回源合并
	redisTTL time.Duration           // Redis 条目缓存 TTL（servers 列表用）
}

var (
	aclServiceOnce sync.Once
	aclService     *AclService
)

// GetAclService 返回全局 ACL 服务（依赖配置已加载，首次调用时构造）
func GetAclService() *AclService {
	aclServiceOnce.Do(func() {
		cfg := config.GetConfig()
		aclService = &AclService{
			redisTTL: cfg.Acl.CacheTtl,
		}
	})
	return aclService
}

func (s *AclService) Name() string { return "acl service" }

// Init 组件启动：全量加载快照，失败即失败启动
func (s *AclService) Init() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.reload(ctx); err != nil {
		return fmt.Errorf("acl snapshot init: %w", err)
	}
	if d := s.snap.Load(); d != nil {
		tlog.Info(ctx, "acl snapshot loaded white:%d black:%d roles:%d", len(d.white), len(d.black), len(d.roles))
	}
	return nil
}

// CheckLoginIP 登录 IP 检测：白名单放行（跳过黑名单），命中黑名单返回 ErrIPBlacklisted
func (s *AclService) CheckLoginIP(ctx context.Context, clientIP string) error {
	if clientIP == "" {
		return nil
	}
	white, err := s.IsWhitelistedIP(ctx, clientIP)
	if err != nil {
		recordAclCheck("ip", "error")
		return err
	}
	if white {
		recordAclCheck("ip", "white")
		return nil
	}
	black, err := s.IsBlacklistedIP(ctx, clientIP)
	if err != nil {
		recordAclCheck("ip", "error")
		return err
	}
	if black {
		recordAclCheck("ip", "block")
		return ErrIPBlacklisted
	}
	recordAclCheck("ip", "pass")
	return nil
}

// CheckRole 封禁角色检测：命中返回 ErrRoleBanned
func (s *AclService) CheckRole(ctx context.Context, roleID string) error {
	if roleID == "" {
		return nil
	}
	banned, err := s.IsRoleBanned(ctx, roleID)
	if err != nil {
		recordAclCheck("role", "error")
		return err
	}
	if banned {
		recordAclCheck("role", "block")
		return ErrRoleBanned
	}
	recordAclCheck("role", "pass")
	return nil
}

// IsWhitelistedIP 查询 IP 是否在白名单（快照读）。先精确查，再扫 CIDR 前缀。
func (s *AclService) IsWhitelistedIP(ctx context.Context, ip string) (bool, error) {
	addr, ok := parseAclAddr(ip)
	if !ok {
		return false, nil
	}
	d, err := s.snapshot(ctx)
	if err != nil {
		return false, err
	}
	if _, hit := d.white[addr.String()]; hit {
		return true, nil
	}
	for _, p := range d.whitePrefix {
		if p.Contains(addr) {
			return true, nil
		}
	}
	return false, nil
}

// IsBlacklistedIP 查询 IP 是否在黑名单（快照读）。先精确查，再扫 CIDR 前缀。
func (s *AclService) IsBlacklistedIP(ctx context.Context, ip string) (bool, error) {
	addr, ok := parseAclAddr(ip)
	if !ok {
		return false, nil
	}
	d, err := s.snapshot(ctx)
	if err != nil {
		return false, err
	}
	if _, hit := d.black[addr.String()]; hit {
		return true, nil
	}
	for _, p := range d.blackPrefix {
		if p.Contains(addr) {
			return true, nil
		}
	}
	return false, nil
}

// IsRoleBanned 查询角色是否被封禁（快照读，零存储依赖）
func (s *AclService) IsRoleBanned(ctx context.Context, roleID string) (bool, error) {
	if roleID == "" {
		return false, nil
	}
	d, err := s.snapshot(ctx)
	if err != nil {
		return false, err
	}
	_, hit := d.roles[roleID]
	return hit, nil
}

// ListWhiteIPs 白名单列表（直查 MySQL，limit<=0 不限制；管理列表接口用）
func (s *AclService) ListWhiteIPs(ctx context.Context, limit int) ([]dmodel.WhitelistIP, error) {
	return dmodel.ListWhitelistIP(ctx, limit)
}

// ListBlackIPs 黑名单列表（直查 MySQL，limit<=0 不限制）
func (s *AclService) ListBlackIPs(ctx context.Context, limit int) ([]dmodel.BlacklistIP, error) {
	return dmodel.ListBlacklistIP(ctx, limit)
}

// ListBannedRoles 封禁角色列表（直查 MySQL，limit<=0 不限制）
func (s *AclService) ListBannedRoles(ctx context.Context, limit int) ([]dmodel.BannedRole, error) {
	return dmodel.ListBannedRoles(ctx, limit)
}

// AddWhiteIP 写入白名单：落库后本地快照即时应用（支持纯 IP 与 CIDR）
func (s *AclService) AddWhiteIP(ctx context.Context, ip, remark, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if err := dmodel.AddWhitelistIP(ctx, norm, remark, operator); err != nil {
		return err
	}
	tlog.Info(ctx, "acl add whitelist ip:%s operator:%s", norm, operator)
	s.applyLocal(rmodel.AclKindWhiteIP, norm, true)
	return nil
}

// RemoveWhiteIP 删除白名单：落库后本地快照即时应用（支持纯 IP 与 CIDR）
func (s *AclService) RemoveWhiteIP(ctx context.Context, ip, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if _, err := dmodel.DelWhitelistIP(ctx, norm); err != nil {
		return err
	}
	tlog.Info(ctx, "acl remove whitelist ip:%s operator:%s", norm, operator)
	s.applyLocal(rmodel.AclKindWhiteIP, norm, false)
	return nil
}

// AddBlackIP 写入黑名单（支持纯 IP 与 CIDR）
func (s *AclService) AddBlackIP(ctx context.Context, ip, remark, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if err := dmodel.AddBlacklistIP(ctx, norm, remark, operator); err != nil {
		return err
	}
	tlog.Info(ctx, "acl add blacklist ip:%s operator:%s", norm, operator)
	s.applyLocal(rmodel.AclKindBlackIP, norm, true)
	// 先落权威库、再 best-effort 踢在线连接（2s 超时/台，失败不回滚）。
	// CIDR 无法用 sess:ip:{exact} 索引路由，改为向全部已知 sgate 下发（每台本地前缀匹配）。
	kickIPAfterBan(ctx, norm)
	return nil
}

// RemoveBlackIP 删除黑名单（支持纯 IP 与 CIDR）
func (s *AclService) RemoveBlackIP(ctx context.Context, ip, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if _, err := dmodel.DelBlacklistIP(ctx, norm); err != nil {
		return err
	}
	tlog.Info(ctx, "acl remove blacklist ip:%s operator:%s", norm, operator)
	s.applyLocal(rmodel.AclKindBlackIP, norm, false)
	return nil
}

// AddBannedRole 封禁角色（HTTP Admin 调用）
func (s *AclService) AddBannedRole(ctx context.Context, roleID, reason, operator string) error {
	if roleID == "" {
		return dmodel.ErrInvalidRoleID
	}
	if err := dmodel.AddBannedRole(ctx, roleID, reason, operator); err != nil {
		return err
	}
	tlog.Info(ctx, "acl add banned role:%s operator:%s", roleID, operator)
	s.applyLocal(rmodel.AclKindRole, roleID, true)
	// 先落权威库、再 best-effort 踢在线连接（2s 超时/台，失败不回滚）
	kickAccountAfterBan(ctx, roleID)
	return nil
}

// RemoveBannedRole 解除角色封禁（HTTP Admin 调用）
func (s *AclService) RemoveBannedRole(ctx context.Context, roleID, operator string) error {
	if roleID == "" {
		return dmodel.ErrInvalidRoleID
	}
	if _, err := dmodel.DelBannedRole(ctx, roleID); err != nil {
		return err
	}
	tlog.Info(ctx, "acl remove banned role:%s operator:%s", roleID, operator)
	s.applyLocal(rmodel.AclKindRole, roleID, false)
	return nil
}

// ServerList 服务器列表（带缓存）
func (s *AclService) ServerList(ctx context.Context) ([]dmodel.ServerInfo, error) {
	if list, ok := s.lookupServers(ctx); ok {
		return list, nil
	}
	v, err, _ := s.sf.Do(rmodel.AclKindServers, func() (any, error) {
		loaderCtx := context.WithoutCancel(ctx)
		if list, ok := s.lookupServers(loaderCtx); ok {
			return list, nil
		}
		list, err := dmodel.ListServers(loaderCtx)
		if err != nil {
			return nil, err
		}
		s.storeServers(loaderCtx, list)
		return list, nil
	})
	if err != nil {
		return nil, err
	}
	list, _ := v.([]dmodel.ServerInfo)
	return list, nil
}

// snapshot 返回当前快照；未初始化（组件 Init 前的极早请求/单测）时同步加载一次
func (s *AclService) snapshot(ctx context.Context) (*aclData, error) {
	if d := s.snap.Load(); d != nil {
		return d, nil
	}
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	if d := s.snap.Load(); d != nil {
		return d, nil
	}
	return nil, errors.New("acl snapshot unavailable")
}

// applyLocal 写路径落地：在本地快照上即时应用增量（读己之写）。
// id 已是 normalizeAclIP 规范化后的字符串（纯 IP 或 CIDR）。
// 全程持有 mu：并发写者串行化，后者 clone 到前者结果，不丢更新。
func (s *AclService) applyLocal(kind, id string, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.snap.Load()
	next := newAclData()
	if old != nil {
		next = old.clone()
	}
	switch kind {
	case rmodel.AclKindWhiteIP:
		if strings.Contains(id, "/") {
			next.applyPrefix(&next.whitePrefix, id, add)
		} else if add {
			next.white[id] = struct{}{}
		} else {
			delete(next.white, id)
		}
	case rmodel.AclKindBlackIP:
		if strings.Contains(id, "/") {
			next.applyPrefix(&next.blackPrefix, id, add)
		} else if add {
			next.black[id] = struct{}{}
		} else {
			delete(next.black, id)
		}
	case rmodel.AclKindRole:
		if add {
			next.roles[id] = struct{}{}
		} else {
			delete(next.roles, id)
		}
	default:
		return
	}
	s.snap.Store(next)
	updateAclSnapshotGauges(next)
}

// reload 全量重载快照（从 MySQL 加载全部白/黑名单/封禁角色）
func (s *AclService) reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	loaderCtx := context.WithoutCancel(ctx)
	whiteRows, err := dmodel.ListWhitelistIP(loaderCtx, 0)
	if err != nil {
		return err
	}
	blackRows, err := dmodel.ListBlacklistIP(loaderCtx, 0)
	if err != nil {
		return err
	}
	roleRows, err := dmodel.ListBannedRoles(loaderCtx, 0)
	if err != nil {
		return err
	}
	d := newAclData()
	for _, row := range whiteRows {
		d.addIPEdge(d.white, &d.whitePrefix, row.IP)
	}
	for _, row := range blackRows {
		d.addIPEdge(d.black, &d.blackPrefix, row.IP)
	}
	for _, row := range roleRows {
		d.roles[row.RoleID] = struct{}{}
	}
	s.snap.Store(d)
	updateAclSnapshotGauges(d)
	return nil
}

func (s *AclService) lookupServers(ctx context.Context) ([]dmodel.ServerInfo, bool) {
	entry, err := rmodel.GetAclEntry(ctx, rmodel.AclKindServers, "")
	if err != nil || entry == nil || len(entry.Data) == 0 {
		return nil, false
	}
	var list []dmodel.ServerInfo
	if err := json.Unmarshal(entry.Data, &list); err != nil {
		return nil, false
	}
	return list, true
}

func (s *AclService) storeServers(ctx context.Context, list []dmodel.ServerInfo) {
	raw, err := json.Marshal(list)
	if err != nil {
		return
	}
	entry := &rmodel.AclEntry{Data: raw}
	if err := rmodel.SetAclEntry(ctx, rmodel.AclKindServers, "", entry, s.redisTTL); err != nil {
		tlog.Warn(ctx, "acl: set server list failed err:%v", err)
	}
}

// normalizeAclIP 规范化 IP 或 CIDR（委托 dmodel.NormalizeIP，单一实现）。
// 非法返回 ok=false。
func normalizeAclIP(ip string) (string, bool) {
	norm, err := dmodel.NormalizeIP(ip)
	if err != nil {
		return "", false
	}
	return norm, true
}

// parseAclAddr 解析查询参数为 netip.Addr（仅接受纯 IP，CIDR 作为查询参数无意义）
func parseAclAddr(ip string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr, true
}

// addIPEdge 按条目形态分流：纯 IP 进 exact 集合，CIDR 解析进前缀切片
func (d *aclData) addIPEdge(exact map[string]struct{}, prefixes *[]netip.Prefix, entry string) {
	if strings.Contains(entry, "/") {
		if p, err := netip.ParsePrefix(entry); err == nil {
			*prefixes = append(*prefixes, p)
		}
		return
	}
	exact[entry] = struct{}{}
}

// applyPrefix 前缀切片的增删（按 String() 精确匹配；生产条目量小，线性扫可接受）
func (d *aclData) applyPrefix(prefixes *[]netip.Prefix, id string, add bool) {
	p, err := netip.ParsePrefix(id)
	if err != nil {
		return
	}
	if !add {
		for i, existing := range *prefixes {
			if existing.String() == p.String() {
				*prefixes = append((*prefixes)[:i], (*prefixes)[i+1:]...)
				return
			}
		}
		return
	}
	for _, existing := range *prefixes {
		if existing.String() == p.String() {
			return // 已存在，幂等
		}
	}
	*prefixes = append(*prefixes, p)
}
