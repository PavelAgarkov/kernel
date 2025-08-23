package readiness_barrier

import "context"

type ReadinessBarrierInterface interface {
	SendSignalCtx(ctx context.Context, sig toggleSignal) error
	IsReady() bool
	Start()
	Stop()
}
