// Package dmodel MySQL 数据访问层（gorm），表结构见 sql/create_tables.sql。
// 业务层经此读写白/黑名单、封禁角色与服务器列表，不直接接触 gorm。
package dmodel

import (
	"context"
	"errors"

	"github.com/streasure/util/ugorm"
	"gorm.io/gorm"
)

// dbKey 业务库在 loginserver.yaml db.configs 下的键名
const dbKey = "logindb"

// ErrInvalidIP 入参 IP 不是合法 IPv4/IPv6
var ErrInvalidIP = errors.New("invalid ip")

// ErrInvalidRoleID 入参角色 ID 为空
var ErrInvalidRoleID = errors.New("invalid role id")

func getDB(ctx context.Context) (*gorm.DB, error) {
	return ugorm.GetDbContext(ctx, dbKey)
}
