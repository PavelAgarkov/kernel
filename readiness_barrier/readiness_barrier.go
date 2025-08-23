package readiness_barrier

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type toggleSignal string

const (
	ReadySignalToggle    toggleSignal = "ready"
	NotReadySignalToggle toggleSignal = "not_ready"
)

type ReadinessBarrierConfig struct {
	Name string
}

type ReadinessBarrier struct {
	config        ReadinessBarrierConfig
	signals       chan toggleSignal // канал для сигналов готовности, явно не закрывается
	readinessFlag atomic.Bool
	parent        context.Context

	running   atomic.Bool
	mu        sync.Mutex
	runCancel context.CancelFunc
	wg        sync.WaitGroup
}

func NewReadinessBarrier(parent context.Context, cfg ReadinessBarrierConfig) *ReadinessBarrier {
	r := &ReadinessBarrier{
		config:  cfg,
		signals: make(chan toggleSignal, 4),
		parent:  parent,
	}
	r.setNotReady()
	return r
}

func (r *ReadinessBarrier) Start() {
	// не даём запустить второй раз, пока уже запущен
	if !r.running.CompareAndSwap(false, true) {
		return
	}

	ctx, cancel := context.WithCancel(r.parent)
	r.mu.Lock()
	r.runCancel = cancel
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.listen(ctx)
	}()

	return
}

func (r *ReadinessBarrier) Stop() {
	if !r.running.CompareAndSwap(true, false) {
		return // уже остановлен
	}

	r.mu.Lock()
	if r.runCancel != nil {
		r.runCancel()
		r.runCancel = nil
	}
	r.mu.Unlock()
	r.wg.Wait()

drop:
	for {
		select {
		case <-r.signals:
		default:
			break drop
		}
	}
	r.setNotReady()
}

func (r *ReadinessBarrier) IsReady() bool {
	return r.readinessFlag.Load()
}

func (r *ReadinessBarrier) SendSignalCtx(ctx context.Context, sig toggleSignal) error {
	if !r.running.Load() {
		return fmt.Errorf("readiness barrier %s: not running", r.config.Name)
	}
	select {
	case r.signals <- sig:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("readiness barrier %s: context cancelled while sending signal", r.config.Name)
	}
}

func (r *ReadinessBarrier) setReady() {
	r.readinessFlag.Store(true)
}

func (r *ReadinessBarrier) setNotReady() {
	r.readinessFlag.Store(false)
}

func (r *ReadinessBarrier) listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-r.signals:
			switch sig {
			case ReadySignalToggle:
				r.setReady()
			case NotReadySignalToggle:
				r.setNotReady()
			}
		}
	}
}
