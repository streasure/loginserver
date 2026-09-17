package pkg

import (
	"sync"
	"testing"
	"time"
)

// newTestTokenCache 创建可注入时间的测试缓存
func newTestTokenCache(ttl time.Duration) (*TokenCache, *time.Time) {
	now := time.Now()
	c := NewTokenCache(ttl)
	c.now = func() time.Time { return now }
	return c, &now
}

func TestTokenCacheMissThenHit(t *testing.T) {
	c, _ := newTestTokenCache(time.Second)

	// 未写入前未命中
	if _, ok := c.Get("acc-1"); ok {
		t.Error("expected miss before Set")
	}

	c.Set("acc-1", "token-1")

	// 写入后命中
	token, ok := c.Get("acc-1")
	if !ok || token != "token-1" {
		t.Errorf("expected hit with 'token-1', got '%s' ok=%v", token, ok)
	}

	// 其他 key 仍未命中
	if _, ok := c.Get("acc-2"); ok {
		t.Error("expected miss for different key")
	}
}

func TestTokenCacheWindowRotation(t *testing.T) {
	c, now := newTestTokenCache(time.Second)

	c.Set("acc-1", "token-1")

	// 窗口 1：命中
	if _, ok := c.Get("acc-1"); !ok {
		t.Error("expected hit in same window")
	}

	// 进入窗口 2：Get 触发 miss（代已过期），Set 触发翻转
	*now = now.Add(2 * time.Second)
	if _, ok := c.Get("acc-1"); ok {
		t.Error("expected miss after window expired (before Set)")
	}

	// 翻转后写入窗口 2 的数据
	c.Set("acc-2", "token-2")

	// acc-1 在 prev 代中，仍应命中（一个窗口的宽限期）
	if token, ok := c.Get("acc-1"); !ok || token != "token-1" {
		t.Errorf("expected prev-window hit for acc-1, got '%s' ok=%v", token, ok)
	}

	// acc-2 在 cur 代中命中
	if token, ok := c.Get("acc-2"); !ok || token != "token-2" {
		t.Errorf("expected cur-window hit for acc-2, got '%s' ok=%v", token, ok)
	}

	// 进入窗口 3：acc-1（窗口 1 的数据）应被整体丢弃
	*now = now.Add(2 * time.Second)
	c.Set("acc-3", "token-3")
	if _, ok := c.Get("acc-1"); ok {
		t.Error("expected acc-1 dropped after two rotations")
	}
	// acc-2（窗口 2）在 prev 代仍命中
	if _, ok := c.Get("acc-2"); !ok {
		t.Error("expected acc-2 still in prev window")
	}
}

func TestTokenCacheNegativeCaching(t *testing.T) {
	c, _ := newTestTokenCache(time.Second)

	// 空串（负缓存：key 不存在的账号）也应以命中返回，
	// 调用方用 "" == loginToken 判定失败，避免打穿 Redis
	c.Set("ghost-account", "")
	token, ok := c.Get("ghost-account")
	if !ok || token != "" {
		t.Errorf("expected negative cache hit with empty token, got '%s' ok=%v", token, ok)
	}
}

func TestTokenCacheOverwrite(t *testing.T) {
	c, _ := newTestTokenCache(time.Second)

	c.Set("acc-1", "token-old")
	c.Set("acc-1", "token-new")

	if token, _ := c.Get("acc-1"); token != "token-new" {
		t.Errorf("expected 'token-new' after overwrite, got '%s'", token)
	}
}

func TestTokenCacheConcurrentAccess(t *testing.T) {
	c, _ := newTestTokenCache(10 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "acc"
			if id%2 == 0 {
				key = "acc-other"
			}
			for j := 0; j < 1000; j++ {
				c.Set(key, "token")
				c.Get(key)
			}
		}(i)
	}
	wg.Wait()
}

func TestTokenCacheGetSetParallelStress(t *testing.T) {
	// 并发读写不同 key，验证翻转竞争下数据竞争安全（-race 下运行）
	c, _ := newTestTokenCache(time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				c.Set("writer", "token")
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				c.Get("writer")
			}
		}()
	}
	wg.Wait()
}
