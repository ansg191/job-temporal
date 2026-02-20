package llm

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
)

const providerHeartbeatInterval = 5 * time.Second

func withActivityHeartbeat(ctx context.Context, interval time.Duration, details func() any, fn func() error) error {
	if interval <= 0 {
		return fn()
	}

	stop := make(chan struct{})
	defer close(stop)

	safeRecordHeartbeat(ctx, details())

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				safeRecordHeartbeat(ctx, details())
			}
		}
	}()

	return fn()
}

func safeRecordHeartbeat(ctx context.Context, details any) {
	defer func() {
		_ = recover()
	}()
	activity.RecordHeartbeat(ctx, details)
}
