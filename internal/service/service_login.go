package service

import (
	"context"
	"errors"

	"github.com/streasure/loginserver/internal/config"
	"github.com/streasure/loginserver/internal/rmodel"

	"github.com/streasure/util/tlog"
	"github.com/streasure/util/uuid"

	redis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// LoginService 登录相关业务逻辑。配置在进程启动时加载完成后不可变，
// 过期时间在调用时读取，无需 Once 缓存。
type LoginService struct {
	singleflight.Group
}

var loginService = &LoginService{}

func GetLoginService() *LoginService {
	return loginService
}

func (s *LoginService) GenerateLoginToken(ctx context.Context, accountId string) (string, error) {
	loginToken := uuid.NewUUID()

	expireSec := config.GetConfig().Limits.LoginTokenExpireSeconds
	if err := rmodel.GetLoginTokenModel().SetLoginToken(ctx, accountId, loginToken, expireSec); err != nil {
		return "", err
	}

	GetTokenManager().Set(accountId, loginToken)

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
	if stored, ok := GetTokenManager().Get(accountId); ok {
		return stored == loginToken, nil
	}

	// 回源不绑定单个请求的生命周期：首个调用方 ctx 取消不应连带失败其他等待方，
	// 回源为单次 Redis GET，由客户端 dial/read/write 超时兜底（通常毫秒级返回）
	loaderCtx := context.WithoutCancel(ctx)
	v, err, _ := s.Do(accountId, func() (any, error) {
		// 合并等待期间缓存可能已被上一轮回源填充，二次检查跳过重复 GET
		if stored, ok := GetTokenManager().Get(accountId); ok {
			return stored, nil
		}
		storedToken, err := rmodel.GetLoginTokenModel().GetLoginToken(loaderCtx, accountId)
		if err != nil {
			return nil, err
		}
		// 回源期间缓存可能已被并发轮换填充（GenerateLoginToken 先写 Redis 再 Set 本地）：
		// 缓存现值与回源值不同说明有更新的 token 已落地，不回填旧值，避免旧 token 复活
		if cur, ok := GetTokenManager().Get(accountId); ok && cur != storedToken {
			return storedToken, nil
		}
		GetTokenManager().Set(accountId, storedToken)
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
	if !GetTokenManager().Enabled() {
		return
	}
	var missing []string
	for _, id := range accountIds {
		if _, ok := GetTokenManager().Get(id); !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return
	}
	tokens, err := rmodel.GetLoginTokenModel().GetLoginTokens(context.WithoutCancel(ctx), missing)
	if err != nil {
		tlog.Warn(ctx, "prefetch login tokens failed count:%d err:%v", len(missing), err)
		return
	}
	for id, tok := range tokens {
		// 与 tokenValid 回源同理：并发轮换已写入更新值时不回填旧值
		if cur, ok := GetTokenManager().Get(id); ok && cur != tok {
			continue
		}
		GetTokenManager().Set(id, tok)
	}
}

func (s *LoginService) BindAccount(ctx context.Context, openId string, ptId int32) (string, error) {
	accountId, err := rmodel.GetAccountModel().GetAccount(ctx, openId, ptId)
	if err != nil {
		return "", err
	}
	if len(accountId) > 0 {
		return accountId, nil
	}

	// SETNX 防并发首登双绑：两个并发 miss 各生成 UUID 后无条件 SET 会互相覆盖，
	// 导致先返回的客户端拿到的 accountId 与最终绑定不一致
	newID := uuid.NewUUID()
	created, err := rmodel.GetAccountModel().SetAccountNX(ctx, openId, ptId, newID)
	if err != nil {
		return "", err
	}
	if created {
		tlog.Info(ctx, "bind openId:%s ptId:%d accountId:%s success", openId, ptId, newID)
		return newID, nil
	}
	// 竞争失败：读取赢家的绑定，保证同一 openId 始终收敛到同一个 accountId
	accountId, err = rmodel.GetAccountModel().GetAccount(ctx, openId, ptId)
	if err != nil {
		return "", err
	}
	if len(accountId) == 0 {
		return "", errors.New("account bind race lost but key missing")
	}
	return accountId, nil
}
