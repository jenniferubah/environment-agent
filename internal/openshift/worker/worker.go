// Package worker provides shared lifecycle helpers for embedded SP apps.
package worker

import (
	"context"
	"sync"
)

// Background runs a cancellable background task once via Start and stops it via Stop.
type Background struct {
	startOnce sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

// Start launches run in a goroutine. onError is called when run returns a non-nil error
// and the context was not cancelled. It is safe to call Start multiple times; only the
// first call runs the task.
func (b *Background) Start(ctx context.Context, run func(ctx context.Context) error, onError func(error)) {
	b.startOnce.Do(func() {
		if run == nil {
			return
		}

		taskCtx, cancel := context.WithCancel(ctx)
		b.cancel = cancel
		done := make(chan struct{})
		b.done = done

		go func() {
			defer close(done)
			if err := run(taskCtx); err != nil && taskCtx.Err() == nil && onError != nil {
				onError(err)
			}
		}()
	})
}

// Stop cancels the running task and waits for it to finish.
func (b *Background) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	if b.done != nil {
		<-b.done
	}
}

// Closer is implemented by NATS publishers and similar resources.
type Closer interface {
	Close() error
}

// Close stops the background task and closes each closer. The first close error is returned.
func Close(bg *Background, closers ...Closer) error {
	if bg != nil {
		bg.Stop()
	}
	var err error
	for _, c := range closers {
		if c == nil {
			continue
		}
		if closeErr := c.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}
