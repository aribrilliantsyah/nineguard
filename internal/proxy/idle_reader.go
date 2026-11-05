package proxy

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

var ErrUpstreamIdleTimeout = errors.New("upstream idle timeout")

// idleTimeoutReader wraps an io.ReadCloser and resets an idle timer whenever bytes are read.
// If no bytes are read for idleDuration, it cancels cancelCtx and causes Read to return ErrUpstreamIdleTimeout.
type idleTimeoutReader struct {
	rc        io.ReadCloser
	idle      time.Duration
	cancel    context.CancelCauseFunc
	ctx       context.Context
	timer     *time.Timer
	mu        sync.Mutex
	timedOut  bool
	closeOnce sync.Once
}

func newIdleTimeoutReader(rc io.ReadCloser, idle time.Duration) (io.ReadCloser, context.Context) {
	ctx, cancel := context.WithCancelCause(context.Background())
	r := &idleTimeoutReader{
		rc:     rc,
		idle:   idle,
		cancel: cancel,
		ctx:    ctx,
	}
	r.timer = time.AfterFunc(idle, func() {
		r.mu.Lock()
		r.timedOut = true
		r.mu.Unlock()
		cancel(ErrUpstreamIdleTimeout)
		_ = rc.Close()
	})
	return r, ctx
}

func (r *idleTimeoutReader) Read(p []byte) (n int, err error) {
	n, err = r.rc.Read(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timedOut {
		return n, ErrUpstreamIdleTimeout
	}
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	if err != nil {
		r.timer.Stop()
	}
	return n, err
}

func (r *idleTimeoutReader) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.timer.Stop()
		r.mu.Unlock()
		r.cancel(nil)
	})
	return r.rc.Close()
}
