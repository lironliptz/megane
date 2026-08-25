package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// BrowserFlow is a high-level browser automation façade: callers use Navigate, Fill, Click,
// etc. without importing rod or Colly. It wraps an internal BrowserSession (rod-backed).
type BrowserFlow struct {
	sess *BrowserSession
}

// OpenBrowserFlow starts Chromium and returns a flow handle. Close when finished.
func OpenBrowserFlow(ctx context.Context, cfg *Config) (*BrowserFlow, error) {
	s, err := OpenBrowserSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &BrowserFlow{sess: s}, nil
}

// Close releases the browser.
func (f *BrowserFlow) Close() {
	if f == nil || f.sess == nil {
		return
	}
	f.sess.Close()
	f.sess = nil
}

// Navigate loads a URL and waits for load + idle (best-effort for SPAs).
func (f *BrowserFlow) Navigate(ctx context.Context, url string) error {
	return f.sess.Navigate(ctx, url)
}

// NavigateLoadIdle loads a URL with load + idle settle but without crawl pacing afterward.
func (f *BrowserFlow) NavigateLoadIdle(ctx context.Context, url string) error {
	return f.sess.NavigateLoadIdle(ctx, url)
}

// WaitVisible waits until selector matches at least one element.
func (f *BrowserFlow) WaitVisible(ctx context.Context, selector string) error {
	return f.sess.WaitVisible(ctx, selector)
}

// PollFirstXPath polls until any xpath matches or timeout (see BrowserSession.PollFirstXPath).
func (f *BrowserFlow) PollFirstXPath(ctx context.Context, xpaths []string, timeout time.Duration) (int, bool) {
	return f.sess.PollFirstXPath(ctx, xpaths, timeout)
}

// Fill types into the first element matching selector.
func (f *BrowserFlow) Fill(ctx context.Context, selector, value string) error {
	return f.sess.Fill(ctx, selector, value)
}

// Click clicks the first element matching selector.
func (f *BrowserFlow) Click(ctx context.Context, selector string) error {
	return f.sess.Click(ctx, selector)
}

// ClickXPath clicks the first element matched by XPath (see BrowserSession.ClickXPath).
func (f *BrowserFlow) ClickXPath(ctx context.Context, xpath string) error {
	return f.sess.ClickXPath(ctx, xpath)
}

// ClickXPathQuick clicks via XPath without crawl pacing — use when you enforce readiness separately.
func (f *BrowserFlow) ClickXPathQuick(ctx context.Context, xpath string) error {
	return f.sess.ClickXPathQuick(ctx, xpath)
}

// ClickCollapseRadioByLabelSubstring finds a label whose text contains needle and clicks its collapse radio.
func (f *BrowserFlow) ClickCollapseRadioByLabelSubstring(ctx context.Context, needle string) error {
	return f.sess.ClickCollapseRadioByLabelSubstring(ctx, needle)
}

// QuickClick clicks without step pacing (use with WaitOpen / fragile SPA flows).
func (f *BrowserFlow) QuickClick(ctx context.Context, selector string) error {
	return f.sess.QuickClick(ctx, selector)
}

// NavigateBack performs history back and updates referer state.
func (f *BrowserFlow) NavigateBack(ctx context.Context) error {
	return f.sess.NavigateBack(ctx)
}

// EvalNumber evaluates JS that returns a number (e.g. `() => document.querySelectorAll('tr').length`).
func (f *BrowserFlow) EvalNumber(ctx context.Context, js string) (int64, error) {
	return f.sess.EvalNumber(ctx, js)
}

// EvalString evaluates JS that returns a string.
func (f *BrowserFlow) EvalString(ctx context.Context, js string) (string, error) {
	return f.sess.EvalString(ctx, js)
}

// HTTPDownload GETs urlStr into cfg.DownloadDir with Referer and Cookie header (same rules as DownloadURLs).
func (f *BrowserFlow) HTTPDownload(ctx context.Context, hc *http.Client, urlStr, referer, cookieHeader string) (destPath string, err error) {
	path, _, err := HTTPDownload(ctx, f.sess.cfg, hc, urlStr, referer, cookieHeader)
	if err != nil {
		return "", err
	}
	if f.sess.cfg.Stats != nil {
		f.sess.cfg.Stats.AddDownloads(1)
	}
	if err := f.sess.cfg.emitDownload(path); err != nil {
		return "", err
	}
	return path, nil
}

// WaitDownloadAfter waits for a browser download after trigger (see BrowserSession.WaitDownloadAfter).
func (f *BrowserFlow) WaitDownloadAfter(ctx context.Context, trigger func() error) (string, error) {
	return f.sess.WaitDownloadAfter(ctx, trigger)
}

// ScrollBottom scrolls to the document bottom.
func (f *BrowserFlow) ScrollBottom(ctx context.Context) error {
	return f.sess.ScrollBottom(ctx)
}

// ScrollNestedContainers scrolls likely overflow regions inside SPAs (see BrowserSession).
func (f *BrowserFlow) ScrollNestedContainers(ctx context.Context) error {
	return f.sess.ScrollNestedContainers(ctx)
}

// LinksHrefMatching returns absolute hrefs for anchors matching cssSelector.
func (f *BrowserFlow) LinksHrefMatching(ctx context.Context, cssSelector string) ([]string, error) {
	return f.sess.LinksHrefMatching(ctx, cssSelector)
}

// DownloadURLs GETs each URL into cfg.DownloadDir.
func (f *BrowserFlow) DownloadURLs(ctx context.Context, hc *http.Client, urls []string) error {
	return f.sess.DownloadURLs(ctx, hc, urls)
}

// Pause applies configured step delay + jitter (politeness between actions).
func (f *BrowserFlow) Pause(ctx context.Context) error {
	log := f.sess.cfg.logger()
	log.Info("crawler", "step", "pause",
		"step_delay", f.sess.cfg.StepDelay.String(),
		"jitter_fraction", f.sess.cfg.JitterFraction)
	return PauseBetweenSteps(ctx, f.sess.cfg)
}

// Sleep blocks for d or until ctx is done (for SPA settle time outside config pacing).
func (f *BrowserFlow) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	f.sess.cfg.logger().Info("crawler", "step", "sleep", "duration", d.String())
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// CurrentURL returns the browser tab URL from DevTools.
func (f *BrowserFlow) CurrentURL() (string, error) {
	info, err := f.sess.page.Info()
	if err != nil {
		return "", err
	}
	return info.URL, nil
}

// RefreshLocation updates the internal referer used for downloads from the live tab URL.
func (f *BrowserFlow) RefreshLocation() error {
	return f.sess.RefreshLocation()
}

// WaitPageLoadedIdle waits for load + idle on the current tab (SPA in-place navigation).
func (f *BrowserFlow) WaitPageLoadedIdle(ctx context.Context, idleDeadline time.Duration) error {
	return f.sess.WaitPageLoadedIdle(ctx, idleDeadline)
}

// WaitURLContains polls until the tab URL contains substr or timeout elapses.
func (f *BrowserFlow) WaitURLContains(ctx context.Context, substr string, timeout time.Duration) error {
	log := f.sess.cfg.logger()
	log.Info("crawler", "step", "wait_url_poll_begin", "substr", substr, "timeout", timeout.String())
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			u, _ := f.CurrentURL()
			return fmt.Errorf("timeout waiting URL to contain %q (last=%q)", substr, u)
		}
		info, err := f.sess.page.Info()
		if err == nil && strings.Contains(info.URL, substr) {
			f.sess.lastURL = info.URL
			log.Info("crawler", "step", "wait_url", "substr", substr, "url", info.URL)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Logger returns the slog logger used by this flow (for app-level messages without importing rod).
func (f *BrowserFlow) Logger() *slog.Logger {
	return f.sess.cfg.logger()
}
