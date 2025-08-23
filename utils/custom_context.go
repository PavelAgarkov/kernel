package utils

import (
	"context"
	"time"
)

type noDeadlineCtx struct{ context.Context }

func (noDeadlineCtx) Deadline() (time.Time, bool) { return time.Time{}, false }

// TimeoutNoDeadline прячем дедлайн с помощью noDeadlineCtx, чтобы драйвер не добавлял SET max_execution_time
func TimeoutNoDeadline(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	tctx, cancel := context.WithTimeout(parent, d)
	return noDeadlineCtx{tctx}, cancel
}
