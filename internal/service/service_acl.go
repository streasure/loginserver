package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/dmodel"
	"github.com/streasure/loginserver/internal/rmodel"
	"github.com/streasure/util/tlog"
	"golang.org/x/sync/singleflight"
)

// ErrIPBlacklisted 登录 IP 命中黑名单
var ErrIPBlacklisted = errors.New("ip blacklisted")

// ErrRoleBanned 角色已被封禁
var ErrRoleBanned = errors.New("role banned")

// aclCache ACL 缓存操作抽象（rmodel 实现），便于单测注入
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

// AclService 黑白名单/封禁角色/服务器列表的读缓存与写穿透。
//
// 缓存协议（版本号 gen token，缓解多节点脏数据）：
//   - 全局版本 acl:gen：每次写库后 INCR，条目携带写入时版本 ver
//   - 读侧维护本地版本快照，每 genRefresh 刷新一次；快照过期的条目强制回源，
//     因此跨节点脏数据窗口 ≈ genRefresh（写入方自身立即生效）
//   - 条目 TTL（cacheTtl）兜底 redis 数据丢失/写失败场景
//   - miss 用 singleflight 合并并发回源；redis 整体不可用时绕过缓存直查 MySQL
type AclService struct {
	cache      aclCache
	ttl        time.Duration
	genRefresh time.Duration

	gen   atomic.Int64 // 本地版本快照
	genAt atomic.Int64 // 快照时间（unixnano）

	sf singleflight.Group
}

var (
	aclServiceOnce sync.Once
	aclService     *AclService
)

// GetAclService 返回全局 ACL 服务（依赖配置与 redis/gorm 组件已初始化，首次调用时构造）
func GetAclService() *AclService {
	aclServiceOnce.Do(func() {
		cfg := config.GetConfig()
		aclService = &AclService{
			cache:      rmodelCache{},
			ttl:        cfg.Acl.CacheTtl,
			genRefresh: cfg.Acl.GenRefresh,
		}
	})
	return aclService
}

// CheckLoginIP 登录 IP 检测：白名单放行（跳过黑名单），命中黑名单返回 ErrIPBlacklisted
func (s *AclService) CheckLoginIP(ctx context.Context, clientIP string) error {
	if clientIP == "" {
		return nil
	}
	white, err := s.IsWhitelistedIP(ctx, clientIP)
	if err != nil {
		return err
	}
	if white {
		return nil
	}
	black, err := s.IsBlacklistedIP(ctx, clientIP)
	if err != nil {
		return err
	}
	if black {
		return ErrIPBlacklisted
	}
	return nil
}

// CheckRole 封禁角色检测：命中返回 ErrRoleBanned
func (s *AclService) CheckRole(ctx context.Context, roleID string) error {
	if roleID == "" {
		return nil
	}
	banned, err := s.IsRoleBanned(ctx, roleID)
	if err != nil {
		return err
	}
	if banned {
		return ErrRoleBanned
	}
	return nil
}

// IsWhitelistedIP 查询 IP 是否在白名单
func (s *AclService) IsWhitelistedIP(ctx context.Context, ip string) (bool, error) {
	return s.checkCached(ctx, rmodel.AclKindWhiteIP, ip, func(ctx context.Context) (bool, error) {
		return dmodel.HasWhitelistIP(ctx, ip)
	})
}

// IsBlacklistedIP 查询 IP 是否在黑名单
func (s *AclService) IsBlacklistedIP(ctx context.Context, ip string) (bool, error) {
	return s.checkCached(ctx, rmodel.AclKindBlackIP, ip, func(ctx context.Context) (bool, error) {
		return dmodel.HasBlacklistIP(ctx, ip)
	})
}

// IsRoleBanned 查询角色是否被封禁
func (s *AclService) IsRoleBanned(ctx context.Context, roleID string) (bool, error) {
	return s.checkCached(ctx, rmodel.AclKindRole, roleID, func(ctx context.Context) (bool, error) {
		return dmodel.HasBannedRole(ctx, roleID)
	})
}

// AddWhiteIP 写入白名单：落库后 bump 版本并写缓存
func (s *AclService) AddWhiteIP(ctx context.Context, ip, remark string) error {
	if err := dmodel.AddWhitelistIP(ctx, ip, remark); err != nil {
		return err
	}
	s.publish(ctx, rmodel.AclKindWhiteIP, ip, true)
	return nil
}

// RemoveWhiteIP 删除白名单：落库后 bump 版本并写负缓存条目（其他节点立即可见删除结果）
func (s *AclService) RemoveWhiteIP(ctx context.Context, ip string) error {
	if _, err := dmodel.DelWhitelistIP(ctx, ip); err != nil {
		return err
	}
	s.publish(ctx, rmodel.AclKindWhiteIP, ip, false)
	return nil
}

// AddBlackIP 写入黑名单
func (s *AclService) AddBlackIP(ctx context.Context, ip, remark string) error {
	if err := dmodel.AddBlacklistIP(ctx, ip, remark); err != nil {
		return err
	}
	s.publish(ctx, rmodel.AclKindBlackIP, ip, true)
	return nil
}

// RemoveBlackIP 删除黑名单
func (s *AclService) RemoveBlackIP(ctx context.Context, ip string) error {
	if _, err := dmodel.DelBlacklistIP(ctx, ip); err != nil {
		return err
	}
	s.publish(ctx, rmodel.AclKindBlackIP, ip, false)
	return nil
}

// AddBannedRole 封禁角色（sgate 经 gRPC 调用）
func (s *AclService) AddBannedRole(ctx context.Context, roleID, reason, operator string) error {
	if err := dmodel.AddBannedRole(ctx, roleID, reason, operator); err != nil {
		return err
	}
	s.publish(ctx, rmodel.AclKindRole, roleID, true)
	return nil
}

// ServerList 服务器列表（带缓存）
func (s *AclService) ServerList(ctx context.Context) ([]dmodel.ServerInfo, error) {
	if list, ok := s.lookupServers(ctx); ok {
		return list, nil
	}
	v, err, _ := s.sf.Do(rmodel.AclKindServers, func() (any, error) {
		if list, ok := s.lookupServers(ctx); ok {
			return list, nil
		}
		list, err := dmodel.ListServers(ctx)
		if err != nil {
			return nil, err
		}
		s.storeServers(ctx, list)
		return list, nil
	})
	if err != nil {
		return nil, err
	}
	list, _ := v.([]dmodel.ServerInfo)
	return list, nil
}

// checkCached 单键读缓存协议：命中且版本不旧于本地快照直接返回，
// 否则 singleflight 合并回源并回填（含负缓存）
func (s *AclService) checkCached(ctx context.Context, kind, id string, load func(context.Context) (bool, error)) (bool, error) {
	if hit, ok := s.lookup(ctx, kind, id); ok {
		return hit, nil
	}
	v, err, _ := s.sf.Do(kind+":"+id, func() (any, error) {
		if hit, ok := s.lookup(ctx, kind, id); ok {
			return hit, nil
		}
		hit, err := load(ctx)
		if err != nil {
			return nil, err
		}
		s.store(ctx, kind, id, hit)
		return hit, nil
	})
	if err != nil {
		return false, err
	}
	hit, _ := v.(bool)
	return hit, nil
}

// lookup 读缓存；返回 ok=false 表示 miss/版本过期/redis 不可用（均回源）
func (s *AclService) lookup(ctx context.Context, kind, id string) (hit bool, ok bool) {
	if err := s.ensureGen(ctx); err != nil {
		tlog.Warn(ctx, "acl: get gen failed, bypass cache kind:%s id:%s err:%v", kind, id, err)
		return false, false
	}
	entry, err := s.cache.GetEntry(ctx, kind, id)
	if err != nil {
		tlog.Warn(ctx, "acl: get entry failed, bypass cache kind:%s id:%s err:%v", kind, id, err)
		return false, false
	}
	if entry == nil || entry.Ver < s.gen.Load() {
		return false, false
	}
	return entry.Hit, true
}

// store 回填条目（best-effort：失败仅告警，下次请求重新回源）
func (s *AclService) store(ctx context.Context, kind, id string, hit bool) {
	entry := &rmodel.AclEntry{Ver: s.gen.Load(), Hit: hit}
	if err := s.cache.SetEntry(ctx, kind, id, entry, s.ttl); err != nil {
		tlog.Warn(ctx, "acl: set entry failed kind:%s id:%s err:%v", kind, id, err)
	}
}

// publish 写路径：INCR 全局版本并写入新条目（删除操作写负缓存条目），
// 其他节点因条目版本更新立即生效；redis 不可用时仅告警（MySQL 为事实源，TTL 兜底收敛）
func (s *AclService) publish(ctx context.Context, kind, id string, hit bool) {
	gen, err := s.cache.IncrGen(ctx)
	if err != nil {
		tlog.Warn(ctx, "acl: incr gen failed kind:%s id:%s err:%v（缓存待 TTL 收敛）", kind, id, err)
		return
	}
	if err := s.cache.SetEntry(ctx, kind, id, &rmodel.AclEntry{Ver: gen, Hit: hit}, s.ttl); err != nil {
		tlog.Warn(ctx, "acl: publish entry failed kind:%s id:%s err:%v", kind, id, err)
	}
}

// ensureGen 本地版本快照过期（genRefresh）时刷新
func (s *AclService) ensureGen(ctx context.Context) error {
	now := time.Now().UnixNano()
	if now-s.genAt.Load() < int64(s.genRefresh) {
		return nil
	}
	gen, err := s.cache.GetGen(ctx)
	if err != nil {
		return err
	}
	s.gen.Store(gen)
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
