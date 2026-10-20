package repository

import (
	"context"
	"time"
)

const timeout = 10 * time.Second

// PingDatabase checks connectivity with a 10-second timeout.
func PingDatabase(ctx context.Context, ping func(context.Context) error) error {
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return ping(connectCtx)
}
