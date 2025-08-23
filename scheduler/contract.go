package scheduler

import (
	"context"
)

type JobSchedulerInterface interface {
	Add(cfg JobConfiguration) error
	Stop() func()
	Start(ctx context.Context) func()
}
