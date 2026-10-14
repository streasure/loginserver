package dmodel

import (
	"context"
	"net"
	"time"

	"gorm.io/gorm/clause"
)

// WhitelistIP 白名单 IP（表 whitelist_ip）
type WhitelistIP struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	IP        string    `gorm:"column:ip;size:45" json:"ip"`
	Remark    string    `gorm:"column:remark;size:255" json:"remark"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (WhitelistIP) TableName() string { return "whitelist_ip" }

// BlacklistIP 黑名单 IP（表 blacklist_ip）
type BlacklistIP struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	IP        string    `gorm:"column:ip;size:45" json:"ip"`
	Remark    string    `gorm:"column:remark;size:255" json:"remark"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (BlacklistIP) TableName() string { return "blacklist_ip" }

func normalizeIP(ip string) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", ErrInvalidIP
	}
	return parsed.String(), nil
}

// AddWhitelistIP 新增白名单 IP（按 ip 唯一键 upsert，重复提交更新备注）
func AddWhitelistIP(ctx context.Context, ip, remark string) error {
	ip, err := normalizeIP(ip)
	if err != nil {
		return err
	}
	db, err := getDB(ctx)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{"remark"}),
	}).Create(&WhitelistIP{IP: ip, Remark: remark}).Error
}

// DelWhitelistIP 删除白名单 IP，返回影响行数
func DelWhitelistIP(ctx context.Context, ip string) (int64, error) {
	ip, err := normalizeIP(ip)
	if err != nil {
		return 0, err
	}
	db, err := getDB(ctx)
	if err != nil {
		return 0, err
	}
	res := db.WithContext(ctx).Where("ip = ?", ip).Delete(&WhitelistIP{})
	return res.RowsAffected, res.Error
}

// HasWhitelistIP 查询 IP 是否在白名单
func HasWhitelistIP(ctx context.Context, ip string) (bool, error) {
	ip, err := normalizeIP(ip)
	if err != nil {
		return false, err
	}
	db, err := getDB(ctx)
	if err != nil {
		return false, err
	}
	var n int64
	if err := db.WithContext(ctx).Model(&WhitelistIP{}).Where("ip = ?", ip).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// AddBlacklistIP 新增黑名单 IP（按 ip 唯一键 upsert，重复提交更新备注）
func AddBlacklistIP(ctx context.Context, ip, remark string) error {
	ip, err := normalizeIP(ip)
	if err != nil {
		return err
	}
	db, err := getDB(ctx)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{"remark"}),
	}).Create(&BlacklistIP{IP: ip, Remark: remark}).Error
}

// DelBlacklistIP 删除黑名单 IP，返回影响行数
func DelBlacklistIP(ctx context.Context, ip string) (int64, error) {
	ip, err := normalizeIP(ip)
	if err != nil {
		return 0, err
	}
	db, err := getDB(ctx)
	if err != nil {
		return 0, err
	}
	res := db.WithContext(ctx).Where("ip = ?", ip).Delete(&BlacklistIP{})
	return res.RowsAffected, res.Error
}

// HasBlacklistIP 查询 IP 是否在黑名单
func HasBlacklistIP(ctx context.Context, ip string) (bool, error) {
	ip, err := normalizeIP(ip)
	if err != nil {
		return false, err
	}
	db, err := getDB(ctx)
	if err != nil {
		return false, err
	}
	var n int64
	if err := db.WithContext(ctx).Model(&BlacklistIP{}).Where("ip = ?", ip).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
