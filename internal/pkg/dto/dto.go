package dto

type Response[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func Success[T any](data T) *Response[T] {
	return &Response[T]{
		Code:    0,
		Message: "",
		Data:    data,
	}
}

func Failure(code int, msg string) *Response[any] {
	return &Response[any]{
		Code:    code,
		Message: msg,
	}
}

func WithCodeMsg(code int, msg string) *Response[any] {
	return &Response[any]{
		Code:    code,
		Message: msg,
	}
}

type LoginReq struct {
	OpenId string `json:"openId" binding:"required"`
	Token  string `json:"token"`
	PtId   int32  `json:"ptid"`
	Mode   int32  `json:"mode"`
}

type LoginAck struct {
	AccountId     string `json:"accountId"`
	LoginToken    string `json:"loginToken"`
	ServerListUrl string `json:"serverListUrl"`
}

type GetServerListReq struct {
	AccountId  string `json:"accountId" binding:"required"`
	LoginToken string `json:"loginToken" binding:"required"`
}

type ServerInfo struct {
	ServerId     int32  `json:"serverId"`
	ServerName   string `json:"serverName"`
	ServerUrl    string `json:"serverUrl"`
	ServerStatus int32  `json:"serverStatus"`
}

type GetServerListAck struct {
	Servers []ServerInfo `json:"servers"`
}

type GetVersionReq struct {
	CenterFlag string `json:"centerFlag"`
}

type GetVersionAck struct {
	LoginServerVersion string `json:"loginServerVersion"`
}

type ValidateLoginTokenReq struct {
	AccountId  string `json:"accountId" binding:"required"`
	LoginToken string `json:"loginToken" binding:"required"`
}

type ValidateLoginTokenAck struct {
	Valid bool `json:"valid"`
}
