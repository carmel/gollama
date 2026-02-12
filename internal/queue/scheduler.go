package queue

import (
	"context"
	"errors"
	"time"
)

type Scheduler struct {
	sem         chan struct{}
	waitTimeout time.Duration
}

func New(maxConcurrency int, waitTimeout time.Duration) *Scheduler {
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	return &Scheduler{
		sem:         make(chan struct{}, maxConcurrency),
		waitTimeout: waitTimeout,
	}
}

func (s *Scheduler) Acquire(ctx context.Context) error {
	if s.waitTimeout <= 0 {
		select {
		case s.sem <- struct{}{}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, s.waitTimeout)
	defer cancel()

	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctxTimeout.Done():
		if errors.Is(ctxTimeout.Err(), context.DeadlineExceeded) {
			return errors.New("queue wait timeout")
		}
		return ctxTimeout.Err()
	}
}

func (s *Scheduler) Release() {
	<-s.sem
}
