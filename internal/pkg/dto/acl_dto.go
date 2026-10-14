package dto

// AclIPReq 白/黑名单增改请求（operator 记录操作人，落库审计）
type AclIPReq struct {
	IP       string `json:"ip" binding:"required"`
	Remark   string `json:"remark"`
	Operator string `json:"operator"`
}

// AclIPDelReq 白/黑名单删除请求（operator 仅审计日志，不落库）
type AclIPDelReq struct {
	IP       string `json:"ip" binding:"required"`
	Operator string `json:"operator"`
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
