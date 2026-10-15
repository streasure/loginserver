package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

// aclCache ACL 存储抽象：gen 版本 + 服务器列表条目（rmodel 实现），便于单测注入。
// 黑白名单/封禁角色已改为进程内全量快照，不再走 per-key 条目。
type aclCache interface {
	GetGen(ctx context.Context) (int64, error)
	IncrGen(ctx context.Context) (int64, error)
	GetEntry(ctx context.Context, kind, id string) (*rmodel.AclEntry, error)
	SetEntry(ctx context.Context, kind, id string, entry *rmodel.AclEntry, ttl time.Duration) error
}

// rmodelCache aclCache 的生产实现，转发 rmodel 包级函数
type rmodelCache struct{}

func (rmodelCache) GetGen(ctx context.Context) (int64, error) {
	return rmodel.GetAclGen(ctx)
}

func (rmodelCache) IncrGen(ctx context.Context) (int64, error) {
	return rmodel.IncrAclGen(ctx)
}

func (rmodelCache) GetEntry(ctx context.Context, kind, id string) (*rmodel.AclEntry, error) {
	return rmodel.GetAclEntry(ctx, kind, id)
}

func (rmodelCache) SetEntry(ctx context.Context, kind, id string, entry *rmodel.AclEntry, ttl time.Duration) error {
	return rmodel.SetAclEntry(ctx, kind, id, entry, ttl)
}

// aclData 全量 ACL 内存快照（白名单/黑名单/封禁角色），copy-on-write 更新
type aclData struct {
	white map[string]struct{}
	black map[string]struct{}
	roles map[string]struct{}
	gen   int64
}

func newAclData(gen int64) *aclData {
	return &aclData{
		white: make(map[string]struct{}),
		black: make(map[string]struct{}),
		roles: make(map[string]struct{}),
		gen:   gen,
	}
}

func (d *aclData) clone() *aclData {
	n := newAclData(d.gen)
	for k := range d.white {
		n.white[k] = struct{}{}
	}
	for k := range d.black {
		n.black[k] = struct{}{}
	}
	for k := range d.roles {
		n.roles[k] = struct{}{}
	}
	return n
}

// aclLoaders 快照数据源（默认 dmodel 直查 MySQL；单测注入内存实现）
type aclLoaders struct {
	ips   func(ctx context.Context) (white, black []string, err error)
	roles func(ctx context.Context) ([]string, error)
}

func defaultAclLoaders() aclLoaders {
	return aclLoaders{
		ips: func(ctx context.Context) ([]string, []string, error) {
			whiteRows, err := dmodel.ListWhitelistIP(ctx, 0)
			if err != nil {
				return nil, nil, err
			}
			blackRows, err := dmodel.ListBlacklistIP(ctx, 0)
			if err != nil {
				return nil, nil, err
			}
			white := make([]string, 0, len(whiteRows))
			for _, r := range whiteRows {
				white = append(white, r.IP)
			}
			black := make([]string, 0, len(blackRows))
			for _, r := range blackRows {
				black = append(black, r.IP)
			}
			return white, black, nil
		},
		roles: func(ctx context.Context) ([]string, error) {
			rows, err := dmodel.ListBannedRoles(ctx, 0)
			if err != nil {
				return nil, err
			}
			roles := make([]string, 0, len(rows))
			for _, r := range rows {
				roles = append(roles, r.RoleID)
			}
			return roles, nil
		},
	}
}

// defaultAclWriteIP 白/黑名单落库（生产实现；单测注入内存实现）
func defaultAclWriteIP(ctx context.Context, kind, ip, remark, operator string, add bool) error {
	if add {
		if kind == rmodel.AclKindWhiteIP {
			return dmodel.AddWhitelistIP(ctx, ip, remark, operator)
		}
		return dmodel.AddBlacklistIP(ctx, ip, remark, operator)
	}
	var err error
	if kind == rmodel.AclKindWhiteIP {
		_, err = dmodel.DelWhitelistIP(ctx, ip)
	} else {
		_, err = dmodel.DelBlacklistIP(ctx, ip)
	}
	return err
}

// defaultAclWriteRole 封禁角色落库（生产实现；单测注入内存实现）
func defaultAclWriteRole(ctx context.Context, roleID, reason, operator string, add bool) error {
	if add {
		return dmodel.AddBannedRole(ctx, roleID, reason, operator)
	}
	_, err := dmodel.DelBannedRole(ctx, roleID)
	return err
}

// AclService 黑白名单/封禁角色/服务器列表服务。
//
// 黑白名单/封禁角色：进程内全量快照，读路径零存储依赖（登录高峰不触 MySQL/Redis）。
//   - 启动 Init 全量加载 MySQL，失败即失败启动（与 MySQL 启动依赖一致）
//   - 写路径：MySQL 事实源 → 本地快照即时应用（copy-on-write，读己之写）→ redis INCR 全局版本广播
//   - 其他节点后台轮询 gen（genRefresh 周期），发现差异 singleflight 全量重载；
//     先读 gen 再读库（写路径先写库再 INCR），加载期间 gen 再涨则下轮继续，不会丢更新
//   - redis 故障：读路径不受影响；gen 轮询告警跳过（继续用现有快照服务），
//     cacheTtl 周期全量重载兜底收敛；启动时 redis 不可用不阻塞（MySQL 仍可加载）
//
// 服务器列表：沿用 per-key 条目缓存（JSON 负载 + gen 版本校验 + singleflight 回源）
type AclService struct {
	component.BaseComponent
	cache      aclCache
	loaders    aclLoaders
	writeIP    func(ctx context.Context, kind, ip, remark, operator string, add bool) error // nil = dmodel
	writeRole  func(ctx context.Context, roleID, reason, operator string, add bool) error   // nil = dmodel
	ttl        time.Duration
	genRefresh time.Duration

	snap     atomic.Pointer[aclData] // 全量快照（copy-on-write 替换）
	gen      atomic.Int64            // 本地观察到的全局版本（servers 条目校验用）
	genAt    atomic.Int64            // gen 最近确认时间（unixnano）
	reloadSF singleflight.Group      // 快照全量重载合并
	sf       singleflight.Group      // servers 回源合并

	stopOnce sync.Once
	stopCh   chan struct{}
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
			cache:      rmodelCache{},
			loaders:    defaultAclLoaders(),
			ttl:        cfg.Acl.CacheTtl,
			genRefresh: cfg.Acl.GenRefresh,
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
		tlog.Info(ctx, "acl snapshot loaded white:%d black:%d roles:%d gen:%d", len(d.white), len(d.black), len(d.roles), d.gen)
	}
	return nil
}

// Start 组件启动：后台 gen 轮询 + 周期全量兜底重载
func (s *AclService) Start() error {
	s.stopCh = make(chan struct{})
	go s.refreshLoop()
	return nil
}

// Destroy 组件销毁：停止后台轮询
func (s *AclService) Destroy() {
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
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

// IsWhitelistedIP 查询 IP 是否在白名单（快照读，零存储依赖）
func (s *AclService) IsWhitelistedIP(ctx context.Context, ip string) (bool, error) {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return false, nil
	}
	d, err := s.snapshot(ctx)
	if err != nil {
		return false, err
	}
	_, hit := d.white[norm]
	return hit, nil
}

// IsBlacklistedIP 查询 IP 是否在黑名单（快照读，零存储依赖）
func (s *AclService) IsBlacklistedIP(ctx context.Context, ip string) (bool, error) {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return false, nil
	}
	d, err := s.snapshot(ctx)
	if err != nil {
		return false, err
	}
	_, hit := d.black[norm]
	return hit, nil
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

// AddWhiteIP 写入白名单：落库后本地快照即时应用并广播版本
func (s *AclService) AddWhiteIP(ctx context.Context, ip, remark, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if err := s.doWriteIP(ctx, rmodel.AclKindWhiteIP, ip, remark, operator, true); err != nil {
		return err
	}
	tlog.Info(ctx, "acl add whitelist ip:%s operator:%s", ip, operator)
	s.applyLocal(ctx, rmodel.AclKindWhiteIP, norm, true)
	return nil
}

// RemoveWhiteIP 删除白名单：落库后本地快照即时应用并广播版本
func (s *AclService) RemoveWhiteIP(ctx context.Context, ip, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if err := s.doWriteIP(ctx, rmodel.AclKindWhiteIP, ip, "", operator, false); err != nil {
		return err
	}
	tlog.Info(ctx, "acl remove whitelist ip:%s operator:%s", ip, operator)
	s.applyLocal(ctx, rmodel.AclKindWhiteIP, norm, false)
	return nil
}

// AddBlackIP 写入黑名单
func (s *AclService) AddBlackIP(ctx context.Context, ip, remark, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if err := s.doWriteIP(ctx, rmodel.AclKindBlackIP, ip, remark, operator, true); err != nil {
		return err
	}
	tlog.Info(ctx, "acl add blacklist ip:%s operator:%s", ip, operator)
	s.applyLocal(ctx, rmodel.AclKindBlackIP, norm, true)
	return nil
}

// RemoveBlackIP 删除黑名单
func (s *AclService) RemoveBlackIP(ctx context.Context, ip, operator string) error {
	norm, ok := normalizeAclIP(ip)
	if !ok {
		return dmodel.ErrInvalidIP
	}
	if err := s.doWriteIP(ctx, rmodel.AclKindBlackIP, ip, "", operator, false); err != nil {
		return err
	}
	tlog.Info(ctx, "acl remove blacklist ip:%s operator:%s", ip, operator)
	s.applyLocal(ctx, rmodel.AclKindBlackIP, norm, false)
	return nil
}

// AddBannedRole 封禁角色（sgate 经 gRPC 调用）
func (s *AclService) AddBannedRole(ctx context.Context, roleID, reason, operator string) error {
	if roleID == "" {
		return dmodel.ErrInvalidRoleID
	}
	if err := s.doWriteRole(ctx, roleID, reason, operator, true); err != nil {
		return err
	}
	tlog.Info(ctx, "acl add banned role:%s operator:%s", roleID, operator)
	s.applyLocal(ctx, rmodel.AclKindRole, roleID, true)
	return nil
}

// RemoveBannedRole 解除角色封禁（sgate 经 gRPC 调用）
func (s *AclService) RemoveBannedRole(ctx context.Context, roleID, operator string) error {
	if roleID == "" {
		return dmodel.ErrInvalidRoleID
	}
	if err := s.doWriteRole(ctx, roleID, "", operator, false); err != nil {
		return err
	}
	tlog.Info(ctx, "acl remove banned role:%s operator:%s", roleID, operator)
	s.applyLocal(ctx, rmodel.AclKindRole, roleID, false)
	return nil
}

// doWriteIP 白/黑名单落库（注入实现优先，默认 dmodel）
func (s *AclService) doWriteIP(ctx context.Context, kind, ip, remark, operator string, add bool) error {
	if s.writeIP != nil {
		return s.writeIP(ctx, kind, ip, remark, operator, add)
	}
	return defaultAclWriteIP(ctx, kind, ip, remark, operator, add)
}

// doWriteRole 封禁角色落库（注入实现优先，默认 dmodel）
func (s *AclService) doWriteRole(ctx context.Context, roleID, reason, operator string, add bool) error {
	if s.writeRole != nil {
		return s.writeRole(ctx, roleID, reason, operator, add)
	}
	return defaultAclWriteRole(ctx, roleID, reason, operator, add)
}

// ServerList 服务器列表（带缓存）
func (s *AclService) ServerList(ctx context.Context) ([]dmodel.ServerInfo, error) {
	if list, ok := s.lookupServers(ctx); ok {
		return list, nil
	}
	v, err, _ := s.sf.Do(rmodel.AclKindServers, func() (any, error) {
		// 回源不绑定单个请求的生命周期：首个调用方 ctx 取消不应连带失败其他等待方
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

// applyLocal 写路径落地：广播版本（INCR）后在本地快照上即时应用增量（读己之写）。
// redis INCR 失败时本地 gen 仍自增先行（其他节点待 cacheTtl 兜底重载收敛）。
func (s *AclService) applyLocal(ctx context.Context, kind, id string, add bool) {
	gen := s.publishGen(ctx)
	old := s.snap.Load()
	next := newAclData(gen)
	if old != nil {
		next = old.clone()
		next.gen = gen
	}
	var m map[string]struct{}
	switch kind {
	case rmodel.AclKindWhiteIP:
		m = next.white
	case rmodel.AclKindBlackIP:
		m = next.black
	case rmodel.AclKindRole:
		m = next.roles
	default:
		return
	}
	if add {
		m[id] = struct{}{}
	} else {
		delete(m, id)
	}
	s.snap.Store(next)
	updateAclSnapshotGauges(next)
}

// publishGen INCR 全局版本并同步本地 gen/genAt；失败时本地自增先行并告警
func (s *AclService) publishGen(ctx context.Context) int64 {
	gen, err := s.cache.IncrGen(ctx)
	if err != nil {
		tlog.Warn(ctx, "acl: incr gen failed err:%v（其他节点待 cacheTtl 兜底收敛）", err)
		gen = s.gen.Load() + 1
	}
	s.gen.Store(gen)
	s.genAt.Store(time.Now().UnixNano())
	return gen
}

// reload 全量重载快照（singleflight 合并）。gen 在读库之前读取并作为快照版本：
// 写路径先写库再 INCR，读库必然包含对应写；若加载期间 gen 再涨，下次轮询继续重载，不会丢更新。
func (s *AclService) reload(ctx context.Context) error {
	start := time.Now()
	_, err, _ := s.reloadSF.Do("snapshot", func() (any, error) {
		// 重载不绑定调用方生命周期：轮询/请求取消不应中断加载
		loaderCtx := context.WithoutCancel(ctx)
		gen, genErr := s.cache.GetGen(loaderCtx)
		if genErr != nil {
			// redis 不可用：内容仍从 MySQL 加载（正确），版本用本地 gen+1 标记（后续轮询会再对齐）
			tlog.Warn(loaderCtx, "acl: get gen failed during reload err:%v", genErr)
			gen = s.gen.Load() + 1
		}
		white, black, err := s.loaders.ips(loaderCtx)
		if err != nil {
			recordAclSnapshotReload("error", time.Since(start))
			return nil, err
		}
		roles, err := s.loaders.roles(loaderCtx)
		if err != nil {
			recordAclSnapshotReload("error", time.Since(start))
			return nil, err
		}
		d := newAclData(gen)
		for _, ip := range white {
			d.white[ip] = struct{}{}
		}
		for _, ip := range black {
			d.black[ip] = struct{}{}
		}
		for _, r := range roles {
			d.roles[r] = struct{}{}
		}
		s.snap.Store(d)
		// 不回退已前进的本地 gen（并发写路径可能已 INCR 更新）；genAt 仅前进
		if gen >= s.gen.Load() {
			s.gen.Store(gen)
		}
		s.genAt.Store(time.Now().UnixNano())
		updateAclSnapshotGauges(d)
		recordAclSnapshotReload("success", time.Since(start))
		return nil, nil
	})
	return err
}

// pollGen 轮询全局版本；差异触发全量重载，redis 故障仅告警（继续用现有快照服务）
func (s *AclService) pollGen() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	gen, err := s.cache.GetGen(ctx)
	if err != nil {
		tlog.Warn(ctx, "acl: poll gen failed err:%v（继续用本地快照服务）", err)
		return
	}
	var cur int64
	if d := s.snap.Load(); d != nil {
		cur = d.gen
	}
	if gen != cur {
		if gen > s.gen.Load() {
			s.gen.Store(gen)
		}
		if err := s.reload(ctx); err != nil {
			tlog.Warn(ctx, "acl: reload after gen change failed err:%v", err)
		}
		return
	}
	s.genAt.Store(time.Now().UnixNano())
}

// refreshLoop 后台循环：genRefresh 轮询版本差异；cacheTtl 周期全量重载兜底（redis 故障/gen 丢失场景）
func (s *AclService) refreshLoop() {
	genTicker := time.NewTicker(s.genRefresh)
	defer genTicker.Stop()
	safetyTicker := time.NewTicker(s.ttl)
	defer safetyTicker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-genTicker.C:
			s.pollGen()
		case <-safetyTicker.C:
			if err := s.reload(context.Background()); err != nil {
				tlog.Warn(context.Background(), "acl: safety reload failed err:%v", err)
			}
		}
	}
}

// ensureGen 本地版本确认时间过期（genRefresh）时刷新（servers 条目校验用；后台轮询在跑时几乎不触发）
func (s *AclService) ensureGen(ctx context.Context) error {
	now := time.Now().UnixNano()
	if now-s.genAt.Load() < int64(s.genRefresh) {
		return nil
	}
	gen, err := s.cache.GetGen(ctx)
	if err != nil {
		return err
	}
	if gen >= s.gen.Load() {
		s.gen.Store(gen)
	}
	s.genAt.Store(now)
	return nil
}

func (s *AclService) lookupServers(ctx context.Context) ([]dmodel.ServerInfo, bool) {
	if err := s.ensureGen(ctx); err != nil {
		return nil, false
	}
	entry, err := s.cache.GetEntry(ctx, rmodel.AclKindServers, "")
	if err != nil || entry == nil || entry.Ver < s.gen.Load() || len(entry.Data) == 0 {
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
	entry := &rmodel.AclEntry{Ver: s.gen.Load(), Data: raw}
	if err := s.cache.SetEntry(ctx, rmodel.AclKindServers, "", entry, s.ttl); err != nil {
		tlog.Warn(ctx, "acl: set server list failed err:%v", err)
	}
}

// normalizeAclIP 规范化 IP（与 dmodel 同口径：net.ParseIP → String）；非法返回 ok=false
func normalizeAclIP(ip string) (string, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", false
	}
	return parsed.String(), true
}
