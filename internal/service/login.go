package service

import (
	"context"
	"errors"
	"loginserver/internal/config"
	"loginserver/internal/rmodel"
	"sync"
	"time"

	"loginserver/internal/pkg"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/tlog"
)

type LoginService struct {
	tokenCache          *pkg.TokenCache
	loginTokenExpireSec int64
}

var (
	loginServiceOnce sync.Once
	loginService     *LoginService
)

// GetLoginService 返回进程级 LoginService 单例
func GetLoginService() *LoginService {
	loginServiceOnce.Do(func() {
		loginService = newLoginService()
	})
	return loginService
}

func newLoginService() *LoginService {
	cfg := config.GetConfig()
	var tokenCache *pkg.TokenCache
	if d, err := time.ParseDuration(cfg.Limits.ValidateTokenCacheTtl); err == nil && d > 0 {
		tokenCache = pkg.NewTokenCache(d)
	}
	return &LoginService{
		tokenCache:          tokenCache,
		loginTokenExpireSec: cfg.Limits.LoginTokenExpireSeconds,
	}
}

func (s *LoginService) GenerateLoginToken(ctx context.Context, accountId string) (string, error) {
	loginToken := uuid.New().String()

	if err := rmodel.SetLoginToken(ctx, accountId, loginToken, s.loginTokenExpireSec); err != nil {
		return "", err
	}

	if s.tokenCache != nil {
		s.tokenCache.Set(accountId, loginToken)
	}

	tlog.Debug(ctx, "generate login token success",
		"accountId", accountId,
		"expireSeconds", s.loginTokenExpireSec,
	)

	return loginToken, nil
}

func (s *LoginService) ValidateLoginToken(ctx context.Context, accountId, loginToken string) (bool, error) {
	if s.tokenCache != nil {
		if stored, ok := s.tokenCache.Get(accountId); ok {
			return stored == loginToken, nil
		}
	}

	storedToken, err := rmodel.GetLoginToken(ctx, accountId)
	if err != nil {
		if errors.Is(err, redis.Nil) && s.tokenCache != nil {
			s.tokenCache.Set(accountId, "")
		}
		return false, err
	}

	if s.tokenCache != nil {
		s.tokenCache.Set(accountId, storedToken)
	}

	return storedToken == loginToken, nil
}

func (s *LoginService) BindAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	accountId, err := rmodel.GetAccount(ctx, openId, ptId)
	if err == nil && len(accountId) > 0 {
		tlog.Debug(ctx, "bind account hit cache",
			"openId", openId,
			"ptId", ptId,
			"accountId", accountId,
		)
		return accountId, nil
	}

	accountId = uuid.New().String()
	if err := rmodel.SetAccount(ctx, openId, ptId, accountId); err != nil {
		return "", err
	}

	tlog.Info(ctx, "bind account success",
		"openId", openId,
		"ptId", ptId,
		"accountId", accountId,
	)

	return accountId, nil
}
