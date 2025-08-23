package locker

import (
	"context"
	"time"
)

type Parameters struct {
	Key, Value                          string
	Expiration, RetryInterval, Deadline time.Duration
}

type (
	Locker interface {
		Lock(ctx context.Context, key, value string, expiration time.Duration) (bool, error)
		Unlock(ctx context.Context, key, value string) (bool, error)
		ExtendLockTTL(ctx context.Context, key, value string, expiration time.Duration) (bool, error)
	}
)
