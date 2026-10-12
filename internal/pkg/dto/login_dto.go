package dto

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
