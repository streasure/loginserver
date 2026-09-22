package service

import (
	"context"
	"loginserver/internal/config"
	"sync"
	"time"

	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
)

type TokenCache struct {
	mu     sync.RWMutex
	cur    map[string]string // 当前窗口的缓存
	prev   map[string]string // 上一窗口的缓存（窗口翻转后仍可读，再翻转即整体丢弃）
	window int64             // cur 所属的窗口编号（now/ttl）
	ttl    time.Duration
	now    func() time.Time
}

func newTokenCache(ttl time.Duration) *TokenCache {
	if ttl <= 0 {
		panic("tokencache: ttl must be positive")
	}
	return &TokenCache{
		cur: make(map[string]string),
		ttl: ttl,
		now: time.Now,
	}
}

// Get 返回缓存的 storedToken；ok=false 表示未命中，调用方应回源 Redis。
// 未命中不代表账号无 token，只是本窗口内还没查过
func (c *TokenCache) Get(accountId string) (token string, ok bool) {
	w := c.now().UnixNano() / int64(c.ttl)

	c.mu.RLock()
	if w != c.window {
		// 缓存代已过期，交由下一次 Set 触发切换
		c.mu.RUnlock()
		return "", false
	}
	token, ok = c.cur[accountId]
	if !ok && c.prev != nil {
		token, ok = c.prev[accountId]
	}
	c.mu.RUnlock()
	return token, ok
}

// Set 写入当前窗口。窗口翻转时把 cur 降级为 prev、新建 cur（双重检查防并发重复翻转）
func (c *TokenCache) Set(accountId, token string) {
	w := c.now().UnixNano() / int64(c.ttl)

	c.mu.Lock()
	if w != c.window {
		c.prev = c.cur
		c.cur = make(map[string]string, len(c.cur))
		c.window = w
	}
	c.cur[accountId] = token
	c.mu.Unlock()
}

type TokenMgr struct {
	component.BaseComponent
	tokenCache *TokenCache

	ctx        context.Context
	cancelFunc context.CancelFunc
}

var tokenManager = &TokenMgr{}

func GetTokenManager() *TokenMgr {
	return tokenManager
}

func (m *TokenMgr) Name() string {
	return "token manager"
}

func (m *TokenMgr) Init() error {
	cfg := config.GetConfig()
	if len(cfg.Limits.ValidateTokenCacheTtl) > 0 {
		if d, err := time.ParseDuration(cfg.Limits.ValidateTokenCacheTtl); err == nil && d > 0 {
			m.tokenCache = newTokenCache(d)
		} else {
			tlog.Warn(context.TODO(), "invalid ValidateTokenCacheTtl:%s, cache disabled err:%v", cfg.Limits.ValidateTokenCacheTtl, err)
		}
	}
	return nil
}

func (m *TokenMgr) Destroy() {
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
}

func (m *TokenMgr) Start() error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	m.cancelFunc = cancelFunc
	m.ctx = ctx
	return nil
}

func (m *TokenMgr) Get(accountId string) (token string, ok bool) {
	if m.tokenCache == nil {
		return "", false // 缓存禁用（TTL 未配置或无效），直接回源 Redis
	}
	return m.tokenCache.Get(accountId)
}

func (m *TokenMgr) Set(accountId, token string) {
	if m.tokenCache == nil {
		return // 缓存禁用
	}
	m.tokenCache.Set(accountId, token)
}
