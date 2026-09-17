package service

import (
	"context"
	"errors"
	"loginserver/internal/config"
	"time"

	"loginserver/internal/pkg"
	"loginserver/internal/pkg/dto"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/uredis"
)

type LoginService struct {
	config *config.Config
	// tokenKeyPrefix 完整的 loginToken key 前缀（belong/serverType:zone:loginToken:），
	// 构造时计算一次，ValidateLoginToken 是热路径，避免每次重复拼接
	tokenKeyPrefix string
	// tokenCache 登录 token 的进程内微缓存，nil 表示禁用（配置 validateTokenCacheTtl）
	tokenCache *pkg.TokenCache
}

func NewLoginService(cfg *config.Config) *LoginService {
	var tokenCache *pkg.TokenCache
	if d, err := time.ParseDuration(cfg.Limits.ValidateTokenCacheTtl); err == nil && d > 0 {
		tokenCache = pkg.NewTokenCache(d)
	}
	return &LoginService{
		config:         cfg,
		tokenKeyPrefix: cfg.Belong + "/" + cfg.ServerType + ":" + cfg.Zone + ":loginToken:",
		tokenCache:     tokenCache,
	}
}

func (s *LoginService) getRedisClient() uredis.Client {
	if comp := component.Get[*uredis.StandaloneComponent](); comp != nil {
		return comp
	}
	return nil
}

func (s *LoginService) GenerateLoginToken(ctx context.Context, accountId string) (string, error) {
	loginToken := uuid.New().String()

	redisClient := s.getRedisClient()
	if redisClient == nil {
		tlog.ErrorContext(ctx, "redis component not found")
		return "", nil
	}

	key := s.tokenKeyPrefix + accountId
	if err := redisClient.Set(ctx, key, loginToken, time.Second*time.Duration(s.config.Limits.LoginTokenExpireSeconds)).Err(); err != nil {
		tlog.ErrorContext(ctx, "redis set login token failed",
			"accountId", accountId,
			"error", err.Error(),
		)
		return "", err
	}

	// 生成新 token 后立即更新缓存，保证随后的校验请求能用新 token 命中
	if s.tokenCache != nil {
		s.tokenCache.Set(accountId, loginToken)
	}

	tlog.DebugContext(ctx, "generate login token success",
		"accountId", accountId,
		"expireSeconds", s.config.Limits.LoginTokenExpireSeconds,
	)

	return loginToken, nil
}

func (s *LoginService) ValidateLoginToken(ctx context.Context, accountId, loginToken string) (bool, error) {
	redisClient := s.getRedisClient()
	if redisClient == nil {
		tlog.ErrorContext(ctx, "redis component not found")
		return false, nil
	}

	// 1. 命中本地缓存：直接与请求携带的 token 比对，无 Redis 往返
	if s.tokenCache != nil {
		if stored, ok := s.tokenCache.Get(accountId); ok {
			return stored == loginToken, nil
		}
	}

	// 2. 回源 Redis
	key := s.tokenKeyPrefix + accountId
	storedToken, err := redisClient.Get(ctx, key).Result()
	if err != nil {
		// 仅对 "key 不存在" 做负缓存（空串），防止无效账号的请求打穿到 Redis；
		// 网络等其他错误不缓存，下一请求仍回源
		if errors.Is(err, redis.Nil) && s.tokenCache != nil {
			s.tokenCache.Set(accountId, "")
		}
		tlog.DebugContext(ctx, "redis get login token failed",
			"accountId", accountId,
			"error", err.Error(),
		)
		return false, err
	}

	if s.tokenCache != nil {
		s.tokenCache.Set(accountId, storedToken)
	}

	return storedToken == loginToken, nil
}

func (s *LoginService) BindAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	redisClient := s.getRedisClient()
	if redisClient == nil {
		tlog.ErrorContext(ctx, "redis component not found")
		return "", nil
	}

	accountKey := pkg.WrapRedisAccountKey(s.config, openId, ptId)

	accountId, err := redisClient.Get(ctx, accountKey).Result()
	if err == nil && len(accountId) > 0 {
		tlog.DebugContext(ctx, "bind account hit cache",
			"openId", openId,
			"ptId", ptId,
			"accountId", accountId,
		)
		return accountId, nil
	}

	accountId = uuid.New().String()
	if err := redisClient.Set(ctx, accountKey, accountId, 0).Err(); err != nil {
		tlog.ErrorContext(ctx, "redis set account failed",
			"openId", openId,
			"ptId", ptId,
			"error", err.Error(),
		)
		return "", err
	}

	tlog.InfoContext(ctx, "bind account success",
		"openId", openId,
		"ptId", ptId,
		"accountId", accountId,
	)

	return accountId, nil
}

func (s *LoginService) GetServerList(ctx context.Context) []dto.ServerInfo {
	servers := make([]dto.ServerInfo, 0, len(s.config.ServerList.ServerInfos))
	for _, info := range s.config.ServerList.ServerInfos {
		servers = append(servers, dto.ServerInfo{
			ServerId:     info.ServerId,
			ServerName:   info.ServerName,
			ServerUrl:    info.ServerUrl,
			ServerStatus: info.ServerStatus,
		})
	}
	return servers
}
