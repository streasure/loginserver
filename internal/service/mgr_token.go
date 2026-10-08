package service

import (
	"context"
	"errors"
	"time"

	"github.com/streasure/loginserver/internal/config"

	"github.com/streasure/util/component"
	"github.com/streasure/util/timewindow"
	"github.com/streasure/util/tlog"
)

// TokenMgr loginToken 校验的进程内缓存（两代时间窗口缓存，见 util/timewindow）。
// ValidateTokenCacheTtl 未配置或非法时缓存禁用，Get/Set 直接回源 Redis。
type TokenMgr struct {
	component.BaseComponent
	cache *timewindow.Cache[string, string]
}

var tokenManager = &TokenMgr{}

func GetTokenManager() *TokenMgr {
	return tokenManager
}

func (m *TokenMgr) Name() string {
	return "token manager"
}

func (m *TokenMgr) Init() error {
	ttl := config.GetConfig().Limits.ValidateTokenCacheTtl
	if ttl == "" {
		return nil // 未配置：缓存禁用
	}
	d, err := time.ParseDuration(ttl)
	if err == nil && d <= 0 {
		err = errors.New("ttl must be positive")
	}
	if err != nil {
		tlog.Warn(context.TODO(), "invalid ValidateTokenCacheTtl:%s, cache disabled err:%v", ttl, err)
		return nil
	}
	m.cache = timewindow.New[string, string](d)
	return nil
}

// Get 返回缓存的 storedToken；ok=false 表示未命中或缓存禁用，调用方应回源 Redis。
// 未命中不代表账号无 token，只是本窗口内还没查过
func (m *TokenMgr) Get(accountId string) (token string, ok bool) {
	if m.cache == nil {
		return "", false
	}
	return m.cache.Get(accountId)
}

// Set 写入当前窗口；缓存禁用时为空操作
func (m *TokenMgr) Set(accountId, token string) {
	if m.cache == nil {
		return
	}
	m.cache.Set(accountId, token)
}
