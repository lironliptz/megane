// Package workqueue provides a small in-memory FIFO job queue for sprouts (crawler ingest, etc.).
package workqueue

import (
	"context"
	"log/slog"
	"sync"
)

// Job is a unit of work executed by the queue.
type Job func(ctx context.Context)

// Queue is a buffered FIFO queue with a fixed worker pool.
type Queue struct {
	ch chan Job
}

// New creates a queue with the given buffer capacity (minimum 1).
func New(buffer int) *Queue {
	if buffer < 1 {
		buffer = 1
	}
	return &Queue{ch: make(chan Job, buffer)}
}

// Enqueue adds a job. Blocks when the buffer is full unless ctx is cancelled.
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	if q == nil || job == nil {
		return nil
	}
	select {
	case q.ch <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run starts worker goroutines that drain the queue until ctx is done.
func (q *Queue) Run(ctx context.Context, workers int) {
	if q == nil {
		return
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-q.ch:
					if !ok {
						return
					}
					if job != nil {
						job(ctx)
					}
				}
			}
		}()
	}
	go func() {
		<-ctx.Done()
		wg.Wait()
	}()
}

// Len returns the number of jobs waiting in the buffer (approximate).
func (q *Queue) Len() int {
	if q == nil {
		return 0
	}
	return len(q.ch)
}

// LogDepth logs queue depth at info level (helper for operators).
func (q *Queue) LogDepth(label string) {
	slog.Info("workqueue depth", "label", label, "pending", q.Len())
}
