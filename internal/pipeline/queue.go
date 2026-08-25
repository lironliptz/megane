package pipeline

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"megane/internal/workqueue"
)

// Queue configures optional serialized/limited-concurrency processing (PR3 hook for sprouts).
type Queue struct {
	inner   *workqueue.Queue
	workers int
}

// InitQueue creates an internal work queue. workers defaults to 1 when <= 0.
func (p *Pipeline) InitQueue(buffer, workers int) {
	if p == nil {
		return
	}
	if buffer < 1 {
		buffer = 64
	}
	if workers < 1 {
		workers = 1
	}
	p.queue = &Queue{inner: workqueue.New(buffer), workers: workers}
}

// StartQueue runs queue workers until ctx is cancelled. No-op if InitQueue was not called.
func (p *Pipeline) StartQueue(ctx context.Context) {
	if p == nil || p.queue == nil || p.queue.inner == nil {
		return
	}
	p.queue.inner.Run(ctx, p.queue.workers)
}

// EnqueueProcess schedules Process for a project via the work queue when configured,
// otherwise runs immediately in a new goroutine (default upload behaviour).
func (p *Pipeline) EnqueueProcess(parent context.Context, projectID int64) {
	if p == nil {
		return
	}
	run := func(ctx context.Context) {
		p.Process(ctx, projectID)
	}
	if p.queue != nil && p.queue.inner != nil {
		go func() {
			_ = p.queue.inner.Enqueue(parent, func(ctx context.Context) { run(ctx) })
		}()
		return
	}
	go run(context.Background())
}

// IngestFromDisk registers an on-disk file as a project and starts processing.
// Useful when files arrive from a crawler or watcher instead of HTTP upload.
func (p *Pipeline) IngestFromDisk(ctx context.Context, userID int64, absPath, originalName, mimeType string, fileSize int64, llmModel string) (int64, error) {
	if p == nil || p.DB == nil {
		return 0, fmt.Errorf("pipeline not configured")
	}
	id, err := p.DB.CreateProject(userID, absPath, originalName, mimeType, llmModel, fileSize)
	if err != nil {
		return 0, err
	}
	_ = p.DB.AddEvent(id, "uploaded", fmt.Sprintf("ingested from disk %q", originalName))
	p.EnqueueProcess(ctx, id)
	return id, nil
}

func defaultQueueWorkers() int {
	raw := os.Getenv("PIPELINE_QUEUE_WORKERS")
	if raw == "" {
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 1
	}
	return n
}
