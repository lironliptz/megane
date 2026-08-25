package workqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueEnqueueRun(t *testing.T) {
	q := New(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Run(ctx, 1)

	var ran atomic.Int32
	err := q.Enqueue(ctx, func(context.Context) { ran.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for ran.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ran.Load() != 1 {
		t.Fatal("job did not run")
	}
}
