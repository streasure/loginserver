package dmodel

import (
	"context"
	"time"
)

// ServerInfo 服务器信息（表 server_info）
type ServerInfo struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ServerID   string    `gorm:"column:server_id;size:64" json:"serverId"`
	ServerName string    `gorm:"column:server_name;size:128" json:"serverName"`
	Zone       string    `gorm:"column:zone;size:32" json:"zone"`
	IP         string    `gorm:"column:ip;size:45" json:"ip"`
	Port       int       `gorm:"column:port" json:"port"`
	Status     int8      `gorm:"column:status" json:"status"`
	Sort       int       `gorm:"column:sort" json:"sort"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (ServerInfo) TableName() string { return "server_info" }

// ListServers 按排序字段返回全部服务器信息
func ListServers(ctx context.Context) ([]ServerInfo, error) {
	db, err := getDB(ctx)
	if err != nil {
		return nil, err
	}
	var list []ServerInfo
	if err := db.WithContext(ctx).Order("sort asc, id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
