package dto

// AclIPReq 白/黑名单增改请求（operator 记录操作人，落库审计）
type AclIPReq struct {
	IP       string `json:"ip" binding:"required,max=45"`
	Remark   string `json:"remark" binding:"max=255"`
	Operator string `json:"operator" binding:"max=64"`
}

// AclIPDelReq 白/黑名单删除请求（operator 仅审计日志，不落库）
type AclIPDelReq struct {
	IP       string `json:"ip" binding:"required,max=45"`
	Operator string `json:"operator" binding:"max=64"`
}

// AclBannedRoleReq 封禁角色请求（roleId 为登录 accountId）
type AclBannedRoleReq struct {
	RoleId   string `json:"roleId" binding:"required,max=64"`
	Reason   string `json:"reason" binding:"max=255"`
	Operator string `json:"operator" binding:"max=64"`
}

// AclBannedRoleDelReq 解除封禁角色请求
type AclBannedRoleDelReq struct {
	RoleId   string `json:"roleId" binding:"required,max=64"`
	Operator string `json:"operator" binding:"max=64"`
}

// ServerAck 服务器列表条目
type ServerAck struct {
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	Zone       string `json:"zone"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Status     int8   `json:"status"`
	Sort       int    `json:"sort"`
}
