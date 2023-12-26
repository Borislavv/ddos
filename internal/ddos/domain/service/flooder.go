package ddosservice

import (
	"context"
	ddos "ddos/config"
	"ddos/internal/ddos/domain/model"
	display "ddos/internal/display/app"
	logservice "ddos/internal/log/domain/service"
	statservice "ddos/internal/stat/domain/service"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type Flooder struct {
	mu  *sync.RWMutex
	ctx context.Context

	respProcCh chan *model.Response
	cfg        *ddos.Config
	display    *display.Display
	logger     *logservice.Logger
	collector  *statservice.Collector
}

func NewFlooder(
	ctx context.Context,
	cfg *ddos.Config,
	display *display.Display,
	logger *logservice.Logger,
	collector *statservice.Collector,
) *Flooder {
	return &Flooder{
		mu:         &sync.RWMutex{},
		respProcCh: make(chan *model.Response, int64(cfg.MaxRPS)*cfg.MaxWorkers),
		ctx:        ctx,
		cfg:        cfg,
		display:    display,
		logger:     logger,
		collector:  collector,
	}
}

func (f *Flooder) Run() {
	lwg := &sync.WaitGroup{}
	defer lwg.Wait()
	lwg.Add(1)
	go f.handleResponsesProcessor(lwg)
	defer close(f.respProcCh)

	wg := &sync.WaitGroup{}
	defer wg.Wait()
	wg.Add(1)
	go f.handleThreadsSpawner(wg, lwg)
}

func (f *Flooder) handleThreadsSpawner(wg *sync.WaitGroup, lwg *sync.WaitGroup) {
	defer wg.Done()

	requestSendTicker := time.NewTicker(time.Second / time.Duration(float64(f.cfg.MaxRPS)*1.10))
	defer requestSendTicker.Stop()

	threadSpawnTicker := time.NewTicker(time.Millisecond * 100)
	defer threadSpawnTicker.Stop()

	for {
		f.collector.SetRPS()

		select {
		case <-f.ctx.Done():
			return
		case <-threadSpawnTicker.C:
			rps := f.collector.RPS()
			f.spawnRequestSenderThread(wg, requestSendTicker, rps)
			f.spawnResponseProcessorThread(lwg, rps)
		}
	}
}

func (f *Flooder) spawnResponseProcessorThread(wg *sync.WaitGroup, crps int64) {
	if crps/3 > f.collector.Processors() && f.collector.Processors() < f.collector.Workers()*3 {
		wg.Add(1)
		go f.handleResponsesProcessor(wg)
		f.collector.AddProcessor()
	}
}

func (f *Flooder) spawnRequestSenderThread(wg *sync.WaitGroup, requestSendTicker *time.Ticker, crps int64) {
	trps := int64(float64(f.cfg.MaxRPS) * 0.95)
	if crps < trps && f.collector.Workers() < f.cfg.MaxWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-f.ctx.Done():
					return
				case <-requestSendTicker.C:
					f.sendRequest()
				}
			}
		}()
		f.collector.AddWorker()
	}
}

func (f *Flooder) sendRequest() {
	rand.Seed(time.Now().UnixNano())

	s := time.Now()
	defer func() {
		f.collector.AddTotal()
		f.collector.AddTotalDuration(time.Since(s))
	}()

	resp, err := http.Get(fmt.Sprintf("%v&ts=%d", f.cfg.URL, rand.Uint64()))
	if err != nil || resp.StatusCode != 200 {
		f.collector.AddFailed()
		f.collector.AddFailedDuration(time.Since(s))
	} else {
		f.collector.AddSuccess()
		f.collector.AddSuccessDuration(time.Since(s))
	}

	f.sendResponseOnProcessing(resp, err)
}

func (f *Flooder) handleResponsesProcessor(wg *sync.WaitGroup) {
	defer wg.Done()
	for response := range f.respProcCh {
		f.processResponse(response)
	}
}

func (f *Flooder) sendResponseOnProcessing(resp *http.Response, err error) {
	f.respProcCh <- &model.Response{
		Resp: resp,
		Err:  err,
	}
}

func (f *Flooder) processResponse(response *model.Response) {
	if response.Err != nil {
		bytes, merr := json.MarshalIndent(model.Log{Error: response.Err.Error()}, "", "  ")
		if merr != nil {
			f.logger.Println(merr.Error())
			return
		}

		f.logger.Println(string(bytes))
	} else if response.Resp.StatusCode != 200 {
		defer func() { _ = response.Resp.Body.Close() }()

		l := model.Log{
			URL:        response.Resp.Request.URL.String(),
			StatusCode: response.Resp.StatusCode,
		}

		if len(f.cfg.LogHeaders) > 0 {
			l.Headers = make(map[string][]string, len(f.cfg.LogHeaders))
			for _, h := range f.cfg.LogHeaders {
				l.Headers[h] = response.Resp.Header.Values(h)
			}
		}

		if f.cfg.ResponseData != "" {
			bytes, rerr := io.ReadAll(response.Resp.Body)
			if rerr != nil {
				f.logger.Println(rerr.Error())
				return
			}
			l.Data = model.Data{
				Expected: f.cfg.ResponseData,
				Gotten:   string(bytes),
			}
		}

		bytes, merr := json.MarshalIndent(l, "", "  ")
		if merr != nil {
			f.logger.Println(merr.Error())
			return
		}

		f.logger.Println(string(bytes))
	} else {
		defer func() { _ = response.Resp.Body.Close() }()

		_, _ = io.Copy(io.Discard, response.Resp.Body)
	}
}
