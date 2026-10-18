package dmodel

import (
	"context"
	"time"

	"gorm.io/gorm/clause"
)

// BannedRole 封禁角色（表 banned_role），roleID 为登录 accountId
type BannedRole struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	RoleID    string    `gorm:"column:role_id;size:64" json:"roleId"`
	Reason    string    `gorm:"column:reason;size:255" json:"reason"`
	Operator  string    `gorm:"column:operator;size:64" json:"operator"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (BannedRole) TableName() string { return "banned_role" }

// AddBannedRole 新增封禁角色（按 role_id 唯一键 upsert，重复封禁更新原因与操作者）
func AddBannedRole(ctx context.Context, roleID, reason, operator string) error {
	if roleID == "" {
		return ErrInvalidRoleID
	}
	db, err := getDB(ctx)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "role_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"reason", "operator"}),
	}).Create(&BannedRole{RoleID: roleID, Reason: reason, Operator: operator}).Error
}

// DelBannedRole 解除角色封禁，返回影响行数
func DelBannedRole(ctx context.Context, roleID string) (int64, error) {
	if roleID == "" {
		return 0, ErrInvalidRoleID
	}
	db, err := getDB(ctx)
	if err != nil {
		return 0, err
	}
	res := db.WithContext(ctx).Where("role_id = ?", roleID).Delete(&BannedRole{})
	return res.RowsAffected, res.Error
}

// ListBannedRoles 封禁角色列表（按 id 升序；limit<=0 不限制；管理列表接口与 ACL 快照全量加载共用）
func ListBannedRoles(ctx context.Context, limit int) ([]BannedRole, error) {
	db, err := getDB(ctx)
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Order("id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []BannedRole
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
