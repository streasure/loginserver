package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/rmodel"

	"github.com/streasure/util/timewindow"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/uuid"

	redis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// LoginService 登录业务逻辑（loginToken 签发/校验 + 账号绑定）。
type LoginService struct {
	sf    singleflight.Group
	once  sync.Once
	cache *timewindow.Cache[string, string] // loginToken 进程内缓存（两代时间窗口），nil = 禁用
}

var loginService = &LoginService{}

func GetLoginService() *LoginService {
	return loginService
}

// initCache 懒初始化 token 进程内缓存（配置在启动时已加载完成，首次校验时解析一次）。
// ValidateTokenCacheTtl 未配置或非法时缓存禁用（cache 保持 nil）。
func (s *LoginService) initCache() {
	s.once.Do(func() {
		ttl := config.GetConfig().Limits.ValidateTokenCacheTtl
		if ttl == "" {
			return // 未配置：缓存禁用
		}
		d, err := time.ParseDuration(ttl)
		if err == nil && d <= 0 {
			err = errors.New("ttl must be positive")
		}
		if err != nil {
			tlog.Warn(context.TODO(), "invalid ValidateTokenCacheTtl:%s, cache disabled err:%v", ttl, err)
			return
		}
		s.cache = timewindow.New[string, string](d)
	})
}

// cachedToken 读进程内缓存；ok=false 表示未命中或缓存禁用，调用方应回源 Redis。
// 未命中不代表账号无 token，只是本窗口内还没查过。
func (s *LoginService) cachedToken(accountId string) (string, bool) {
	s.initCache()
	if s.cache == nil {
		return "", false
	}
	return s.cache.Get(accountId)
}

// setCachedToken 写进程内缓存；缓存禁用时为空操作
func (s *LoginService) setCachedToken(accountId, token string) {
	s.initCache()
	if s.cache == nil {
		return
	}
	s.cache.Set(accountId, token)
}

func (s *LoginService) GenerateLoginToken(ctx context.Context, accountId string) (string, error) {
	loginToken := uuid.NewUUID()

	expireSec := config.GetConfig().Limits.LoginTokenExpireSeconds
	if err := rmodel.SetLoginToken(ctx, accountId, loginToken, expireSec); err != nil {
		return "", err
	}

	s.setCachedToken(accountId, loginToken)

	// token 是 bearer 凭证，不落日志
	tlog.Info(ctx, "account:%s generate login token success", accountId)

	return loginToken, nil
}

func (s *LoginService) ValidateLoginToken(ctx context.Context, accountId, loginToken string) (bool, error) {
	valid, err := s.tokenValid(ctx, accountId, loginToken)
	if err != nil || !valid {
		return false, err
	}
	// token 有效仍检测封禁：封禁后已登录会话重校验即失效（不踢在线，重连即拦）；
	// 检测故障 fail-closed，与登录路径一致
	banned, err := GetAclService().IsRoleBanned(ctx, accountId)
	if err != nil {
		return false, err
	}
	if banned {
		tlog.Warn(ctx, "validate blocked, account:%s banned", accountId)
		return false, nil
	}
	return true, nil
}

// tokenValid 校验 loginToken 是否与存储值一致（进程内缓存优先，miss 回源 redis）
func (s *LoginService) tokenValid(ctx context.Context, accountId, loginToken string) (bool, error) {
	if stored, ok := s.cachedToken(accountId); ok {
		return stored == loginToken, nil
	}

	// 回源不绑定单个请求的生命周期：首个调用方 ctx 取消不应连带失败其他等待方，
	// 回源为单次 Redis GET，由客户端 dial/read/write 超时兜底（通常毫秒级返回）
	loaderCtx := context.WithoutCancel(ctx)
	v, err, _ := s.sf.Do(accountId, func() (any, error) {
		// 合并等待期间缓存可能已被上一轮回源填充，二次检查跳过重复 GET
		if stored, ok := s.cachedToken(accountId); ok {
			return stored, nil
		}
		storedToken, err := rmodel.GetLoginToken(loaderCtx, accountId)
		if err != nil {
			return nil, err
		}
		// 回源期间缓存可能已被并发轮换填充（GenerateLoginToken 先写 Redis 再 Set 本地）：
		// 缓存现值与回源值不同说明有更新的 token 已落地，不回填旧值，避免旧 token 复活
		if cur, ok := s.cachedToken(accountId); ok && cur != storedToken {
			return storedToken, nil
		}
		s.setCachedToken(accountId, storedToken)
		return storedToken, nil
	})
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}
	storedToken, _ := v.(string)
	return storedToken == loginToken, nil
}

// PrefetchLoginTokens 批量预取缺失的 loginToken 到进程内缓存（BatchValidate 等批量校验前置）。
// 已命中缓存的条目跳过；MGET 合并一次往返，失败仅告警（调用方逐条回源路径兜底）。
func (s *LoginService) PrefetchLoginTokens(ctx context.Context, accountIds []string) {
	// 缓存禁用时预取无处可存（后续校验仍逐条回源），跳过以免每批多一次无用 MGET
	s.initCache()
	if s.cache == nil {
		return
	}
	var missing []string
	for _, id := range accountIds {
		if _, ok := s.cachedToken(id); !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return
	}
	tokens, err := rmodel.GetLoginTokens(context.WithoutCancel(ctx), missing)
	if err != nil {
		tlog.Warn(ctx, "prefetch login tokens failed count:%d err:%v", len(missing), err)
		return
	}
	for id, tok := range tokens {
		// 与 tokenValid 回源同理：并发轮换已写入更新值时不回填旧值
		if cur, ok := s.cachedToken(id); ok && cur != tok {
			continue
		}
		s.setCachedToken(id, tok)
	}
}

func (s *LoginService) BindAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	accountId, err := rmodel.GetAccount(ctx, openId, ptId)
	if err != nil {
		return "", err
	}
	if len(accountId) > 0 {
		return accountId, nil
	}

	// SETNX 防并发首登双绑：两个并发 miss 各生成 UUID 后无条件 SET 会互相覆盖，
	// 导致先返回的客户端拿到的 accountId 与最终绑定不一致
	newID := uuid.NewUUID()
	created, err := rmodel.SetAccountNX(ctx, openId, ptId, newID)
	if err != nil {
		return "", err
	}
	if created {
		tlog.Info(ctx, "bind openId:%s ptId:%d accountId:%s success", openId, ptId, newID)
		return newID, nil
	}
	// 竞争失败：读取赢家的绑定，保证同一 openId 始终收敛到同一个 accountId
	accountId, err = rmodel.GetAccount(ctx, openId, ptId)
	if err != nil {
		return "", err
	}
	if len(accountId) == 0 {
		return "", errors.New("account bind race lost but key missing")
	}
	return accountId, nil
}
