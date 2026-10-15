package component

import (
	"github.com/streasure/util/component"
	"github.com/streasure/util/uload"
)

// LoadComponent 将 uload 自适应准入的 Obtain/Release 挂载到组件生命周期。
// util 解耦后 ugin/ugrpc 不再隐式接线：gin 中间件经 ugin.WithPreMiddleware
// (uload.Middleware()) 注入，gRPC 拦截器经 ugrpc.WithUnaryInterceptors 注入。
type LoadComponent struct {
	component.BaseComponent
	cfg     uload.Config
	release func()
}

func NewLoadComponent(cfg uload.Config) *LoadComponent {
	return &LoadComponent{cfg: cfg}
}

func (l *LoadComponent) Name() string {
	return "load"
}

func (l *LoadComponent) Init() error {
	release, err := uload.Obtain(l.cfg)
	if err != nil {
		return err
	}
	l.release = release
	return nil
}

func (l *LoadComponent) Destroy() {
	if l.release != nil {
		l.release()
		l.release = nil
	}
}
