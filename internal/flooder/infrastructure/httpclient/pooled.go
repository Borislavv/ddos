package httpclient

import (
	config "github.com/Borislavv/go-ddos/internal/flooder/infrastructure/httpclient/config"
	middleware "github.com/Borislavv/go-ddos/internal/flooder/infrastructure/httpclient/middleware"
	model "github.com/Borislavv/go-ddos/internal/flooder/infrastructure/httpclient/model"
	"net/http"
	"runtime"
	"sync/atomic"
)

type CancelFunc func()

type Pool struct {
	cfg     *config.Config
	pool    chan *http.Client
	creator func() *http.Client
	req     middleware.RequestModifier
	resp    middleware.ResponseHandler
	conns   int64
}

func NewPool(cfg *config.Config, creator func() *http.Client) *Pool {
	if cfg.PoolInitSize > cfg.PoolMaxSize {
		cfg.PoolInitSize = cfg.PoolMaxSize
	}

	p := &Pool{
		cfg:     cfg,
		pool:    make(chan *http.Client, cfg.PoolMaxSize),
		creator: creator,
	}

	for i := int64(0); i < cfg.PoolInitSize; i++ {
		p.pool <- creator()
	}

	p.conns = cfg.PoolInitSize

	p.req = middleware.RequestModifierFunc(
		func(req *http.Request) (resp *http.Response, err error) {
			c := p.get()
			defer p.put(c)
			return c.Do(req)
		},
	)

	p.resp = middleware.ResponseHandlerFunc(
		func(resp *model.Response) *model.Response {
			return resp
		},
	)

	return p
}

func (p *Pool) Do(req *http.Request) (resp *http.Response, err error) {
	return p.resp.Handle(model.NewResponse().SetResponse(p.req.Do(req))).Response()
}

func (p *Pool) OnReq(middlewares ...middleware.RequestMiddlewareFunc) Pooled {
	for i := len(middlewares) - 1; i >= 0; i-- {
		p.req = middlewares[i].Exec(p.req)
	}
	return p
}

func (p *Pool) OnResp(middlewares ...middleware.ResponseMiddlewareFunc) Pooled {
	for i := len(middlewares) - 1; i >= 0; i-- {
		p.resp = middlewares[i].Exec(p.resp)
	}
	return p
}

func (p *Pool) Busy() int64 {
	return p.Total() - int64(len(p.pool))
}

func (p *Pool) Total() int64 {
	t := atomic.LoadInt64(&p.conns)
	if t > p.cfg.PoolMaxSize {
		return p.cfg.PoolMaxSize
	}
	return t
}

func (p *Pool) OutOfPool() int64 {
	return atomic.LoadInt64(&p.conns) - int64(cap(p.pool))
}

func (p *Pool) get() *http.Client {
	select {
	case c := <-p.pool:
		return c
	default:
		for {
			conns := atomic.LoadInt64(&p.conns)
			if conns >= p.cfg.PoolMaxSize {
				break
			}

			if atomic.CompareAndSwapInt64(&p.conns, conns, conns+1) {
				return p.creator()
			}
		}
		return <-p.pool
	}
}

func (p *Pool) put(c *http.Client) {
	select {
	case p.pool <- c:
	default:
		runtime.Gosched()
	}
}
