package dmodel

import (
	"context"
	"time"

	"github.com/streasure/util/netutil"
	"gorm.io/gorm/clause"
)

// WhitelistIP 白名单 IP（表 whitelist_ip）
type WhitelistIP struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	IP        string    `gorm:"column:ip;size:45" json:"ip"`
	Remark    string    `gorm:"column:remark;size:255" json:"remark"`
	Operator  string    `gorm:"column:operator;size:64" json:"operator"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (WhitelistIP) TableName() string { return "whitelist_ip" }

// BlacklistIP 黑名单 IP（表 blacklist_ip）
type BlacklistIP struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	IP        string    `gorm:"column:ip;size:45" json:"ip"`
	Remark    string    `gorm:"column:remark;size:255" json:"remark"`
	Operator  string    `gorm:"column:operator;size:64" json:"operator"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (BlacklistIP) TableName() string { return "blacklist_ip" }

// NormalizeIP 规范化 IP 或 CIDR（委托 netutil.NormalizeIPOrCIDR，单一实现）：
// 普通 IP 转 netip 规范字符串；CIDR 先掩码掉 host bits 再转规范字符串
// （保证 1.2.3.4/24 与 1.2.3.0/24 收敛为同一唯一键，否则按"等价写法"删除
// 会因 ip 等值匹配不中而残留）。非法返回 ErrInvalidIP。
func NormalizeIP(ip string) (string, error) {
	return netutil.NormalizeIPOrCIDR(ip)
}

// AddWhitelistIP 新增白名单 IP（按 ip 唯一键 upsert，重复提交更新备注与操作者）
func AddWhitelistIP(ctx context.Context, ip, remark, operator string) error {
	ip, err := NormalizeIP(ip)
	if err != nil {
		return err
	}
	db, err := getDB(ctx)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{"remark", "operator"}),
	}).Create(&WhitelistIP{IP: ip, Remark: remark, Operator: operator}).Error
}

// DelWhitelistIP 删除白名单 IP，返回影响行数
func DelWhitelistIP(ctx context.Context, ip string) (int64, error) {
	ip, err := NormalizeIP(ip)
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

// AddBlacklistIP 新增黑名单 IP（按 ip 唯一键 upsert，重复提交更新备注与操作者）
func AddBlacklistIP(ctx context.Context, ip, remark, operator string) error {
	ip, err := NormalizeIP(ip)
	if err != nil {
		return err
	}
	db, err := getDB(ctx)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{"remark", "operator"}),
	}).Create(&BlacklistIP{IP: ip, Remark: remark, Operator: operator}).Error
}

// DelBlacklistIP 删除黑名单 IP，返回影响行数
func DelBlacklistIP(ctx context.Context, ip string) (int64, error) {
	ip, err := NormalizeIP(ip)
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

// ListWhitelistIP 白名单列表（按 id 升序；limit<=0 不限制；管理列表接口与 ACL 快照全量加载共用）
func ListWhitelistIP(ctx context.Context, limit int) ([]WhitelistIP, error) {
	db, err := getDB(ctx)
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Order("id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []WhitelistIP
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListBlacklistIP 黑名单列表（按 id 升序；limit<=0 不限制）
func ListBlacklistIP(ctx context.Context, limit int) ([]BlacklistIP, error) {
	db, err := getDB(ctx)
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Order("id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []BlacklistIP
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
