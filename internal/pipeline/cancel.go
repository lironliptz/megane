package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type projectCancel struct {
	cancel context.CancelFunc
}

// beginProjectRun returns a cancellable context for Process. release must run when the goroutine exits.
func (p *Pipeline) beginProjectRun(parent context.Context, projectID int64) (context.Context, func()) {
	if p == nil || projectID < 1 {
		return parent, func() {}
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	p.projectCancels.Store(projectID, &projectCancel{cancel: cancel})
	return ctx, func() {
		p.projectCancels.Delete(projectID)
	}
}

// CancelProject stops an in-flight run (if any) and marks the project cancelled in the DB.
func (p *Pipeline) CancelProject(ctx context.Context, projectID int64, reason string) error {
	if p == nil || p.DB == nil || projectID < 1 {
		return fmt.Errorf("pipeline not configured")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled by user"
	}
	if v, ok := p.projectCancels.Load(projectID); ok {
		if c, ok := v.(*projectCancel); ok && c != nil && c.cancel != nil {
			c.cancel()
		}
	}
	p.projectCancels.Delete(projectID)
	p.projectBusy.Delete(projectID)

	if err := p.DB.CancelProjectProcessing(ctx, projectID, reason); err != nil {
		return err
	}
	slog.Info("project cancelled", "project_id", projectID)
	return nil
}

func isContextCancelled(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled")
}