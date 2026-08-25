package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/gocolly/colly/v2"
)

// StaticClient uses Colly for pages that do not require JavaScript execution.
type StaticClient struct {
	cfg *Config
}

// NewStaticClient builds a Colly-backed fetcher.
func NewStaticClient(cfg *Config) *StaticClient {
	return &StaticClient{cfg: cfg}
}

// FetchHTML performs GET urlStr and returns the raw HTML body.
func (s *StaticClient) FetchHTML(ctx context.Context, urlStr string) (string, error) {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "navigate", "url", urlStr, "backend", string(BackendStatic))

	var body []byte
	c := colly.NewCollector()
	if s.cfg.UserAgent != "" {
		c.UserAgent = s.cfg.UserAgent
	}
	c.OnResponse(func(r *colly.Response) {
		body = append([]byte(nil), r.Body...)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Visit(urlStr)
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-errCh:
		if err != nil {
			return "", fmt.Errorf("colly visit: %w", err)
		}
	}

	logStepDone(log, "navigate", start, "url", urlStr, "bytes", len(body))
	if err := PauseBetweenSteps(ctx, s.cfg); err != nil {
		return "", err
	}
	return string(body), nil
}
