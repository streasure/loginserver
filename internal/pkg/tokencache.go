package pkg

import (
	"sync"
	"time"
)

// TokenCache 登录 token 的进程内微缓存。
// 目的：消除热路径上每请求一次的 Redis GET 往返（压测显示约 1/3 的 CPU 耗在 Redis socket syscall）。
//
// 语义：缓存 "accountId -> Redis 中存储的 token"（不缓存校验结果），
// 每次校验仍与请求携带的 token 比对，因此 token 轮换后新 token 立即可用；
// token 撤销（删除 Redis key）的生效延迟上界为一个 TTL 窗口。
//
// 实现：双 map 分代 + RWMutex。读路径全部读锁；每个 TTL 窗口只发生一次
// 代际切换（写锁内做 map 指针交换），过期数据随代际切换整体丢弃，无逐条清理开销。
type TokenCache struct {
	mu     sync.RWMutex
	cur    map[string]string // 当前窗口的缓存
	prev   map[string]string // 上一窗口的缓存（窗口翻转后仍可读，再翻转即整体丢弃）
	window int64             // cur 所属的窗口编号（now/ttl）
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenCache 创建 token 缓存。ttl 必须大于 0
func NewTokenCache(ttl time.Duration) *TokenCache {
	return &TokenCache{
		cur:  make(map[string]string),
		ttl:  ttl,
		now:  time.Now,
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
