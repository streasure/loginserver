package internal

import (
	"sync"
)

var initOnce sync.Once

var _options *Options

func InitOptions(opts ...Option) *Options {
	initOnce.Do(func() {
		_options = &Options{}

		for _, opt := range opts {
			opt(_options)
		}
	})

	return _options
}

func GetOptions() *Options {
	return _options
}

type Option func(*Options)

type Options struct {
	Belong                  string
	ServerType              string
	Zone                    string
	ServerId                string
	LoginTokenExpireSeconds int64
	ClientGetServerListUrl  string
	ServerInfos             []ServerInfo
	HttpAddr                string
	GrpcServiceAddr         string
}

func WithBelong(belong string) Option {
	return func(o *Options) {
		o.Belong = belong
	}
}

func WithServerType(serverType string) Option {
	return func(o *Options) {
		o.ServerType = serverType
	}
}

func WithZone(zone string) Option {
	return func(o *Options) {
		o.Zone = zone
	}
}

func WithServerId(serverId string) Option {
	return func(o *Options) {
		o.ServerId = serverId
	}
}

func WithLoginTokenExpireSeconds(seconds int64) Option {
	return func(o *Options) {
		o.LoginTokenExpireSeconds = seconds
	}
}

func WithClientGetServerListUrl(url string) Option {
	return func(o *Options) {
		o.ClientGetServerListUrl = url
	}
}

func WithServerInfos(infos []ServerInfo) Option {
	return func(o *Options) {
		o.ServerInfos = infos
	}
}

func WithHttpAddr(addr string) Option {
	return func(o *Options) {
		o.HttpAddr = addr
	}
}

func WithGrpcServiceAddr(addr string) Option {
	return func(o *Options) {
		o.GrpcServiceAddr = addr
	}
}
