package service

import (
	"context"
	"loginserver/internal/config"
	"time"

	"loginserver/internal/pkg"
	"loginserver/internal/pkg/dto"

	"github.com/google/uuid"
	"github.com/streasure/util/component"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/uredis"
)

type LoginService struct {
	config *config.Config
}

func NewLoginService(cfg *config.Config) *LoginService {
	return &LoginService{
		config: cfg,
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

	key := pkg.WrapRedisLoginTokenKey(s.config, accountId)
	if err := redisClient.Set(ctx, key, loginToken, time.Second*time.Duration(s.config.Limits.LoginTokenExpireSeconds)).Err(); err != nil {
		tlog.ErrorContext(ctx, "redis set login token failed",
			"accountId", accountId,
			"error", err.Error(),
		)
		return "", err
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

	key := pkg.WrapRedisLoginTokenKey(s.config, accountId)
	storedToken, err := redisClient.Get(ctx, key).Result()
	if err != nil {
		tlog.DebugContext(ctx, "redis get login token failed",
			"accountId", accountId,
			"error", err.Error(),
		)
		return false, err
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
