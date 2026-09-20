package dto

// 业务错误码
const (
	CodeParamError        = 1001
	CodeBindAccountFailed = 1002
	CodeTokenGenFailed    = 1003
)

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

type LoginReq struct {
	OpenId string `json:"openId" binding:"required"`
	PtId   int32  `json:"ptid"`
}

type LoginAck struct {
	AccountId  string `json:"accountId"`
	LoginToken string `json:"loginToken"`
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
