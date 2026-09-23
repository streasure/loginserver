package service

import (
	"context"
	"errors"
	"loginserver/internal/config"
	"loginserver/internal/rmodel"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/streasure/util/tlog"
	"github.com/streasure/util/uuid"
)

type LoginService struct {
	once                sync.Once
	loginTokenExpireSec int64
}

var loginService = &LoginService{}

func GetLoginService() *LoginService {
	loginService.once.Do(func() {
		loginService.loginTokenExpireSec = config.GetConfig().Limits.LoginTokenExpireSeconds
	})
	return loginService
}

func (s *LoginService) GenerateLoginToken(ctx context.Context, accountId string) (string, error) {
	loginToken := uuid.NewUUID()

	if err := rmodel.GetLoginTokenModel().SetLoginToken(ctx, accountId, loginToken, s.loginTokenExpireSec); err != nil {
		return "", err
	}

	GetTokenManager().Set(accountId, loginToken)

	tlog.Info(ctx, "account:%s generate login token:%s success", accountId, loginToken)

	return loginToken, nil
}

func (s *LoginService) ValidateLoginToken(ctx context.Context, accountId, loginToken string) (bool, error) {
	if stored, ok := GetTokenManager().Get(accountId); ok {
		return stored == loginToken, nil
	}

	storedToken, err := rmodel.GetLoginTokenModel().GetLoginToken(ctx, accountId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}

	GetTokenManager().Set(accountId, storedToken)

	return storedToken == loginToken, nil
}

func (s *LoginService) BindAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	accountId, err := rmodel.GetAccountModel().GetAccount(ctx, openId, ptId)
	if err != nil {
		return "", err
	}
	if len(accountId) > 0 {
		return accountId, nil
	}

	accountId = uuid.NewUUID()
	if err := rmodel.GetAccountModel().SetAccount(ctx, openId, ptId, accountId); err != nil {
		return "", err
	}

	tlog.Info(ctx, "bind openId:%s ptId:%s accountId:%s success", openId, ptId, accountId)

	return accountId, nil
}
