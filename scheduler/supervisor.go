package scheduler

import (
	"context"
)

type TaskSupervisor struct {
	Schedulers []JobSchedulerInterface
}

func NewTaskSupervisor(schedulers []JobSchedulerInterface) *TaskSupervisor {
	return &TaskSupervisor{
		Schedulers: schedulers,
	}
}

func (c *TaskSupervisor) Start(ctx context.Context) {
	for _, scheduler := range c.Schedulers {
		scheduler.Start(ctx)()
	}
}

func (c *TaskSupervisor) Stop() {
	for _, scheduler := range c.Schedulers {
		scheduler.Stop()()
	}
}
