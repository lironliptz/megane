package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// BrowserSession runs imperative steps in one rod Page with pacing and slog logging.
type BrowserSession struct {
	cfg     *Config
	browser *rod.Browser
	page    *rod.Page
	lastURL string
}

// OpenBrowserSession launches Chromium and opens a tab.
func OpenBrowserSession(ctx context.Context, cfg *Config) (*BrowserSession, error) {
	b, err := LaunchBrowser(ctx, cfg)
	if err != nil {
		return nil, err
	}
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		_ = b.Close()
		return nil, fmt.Errorf("new page: %w", err)
	}
	if cfg.UserAgent != "" {
		if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: cfg.UserAgent}); err != nil {
			_ = page.Close()
			_ = b.Close()
			return nil, fmt.Errorf("set user agent: %w", err)
		}
	}
	return &BrowserSession{cfg: cfg, browser: b, page: page}, nil
}

// Close tears down the page and browser.
func (s *BrowserSession) Close() {
	if s == nil {
		return
	}
	if s.page != nil {
		_ = s.page.Close()
		s.page = nil
	}
	if s.browser != nil {
		_ = s.browser.Close()
		s.browser = nil
	}
}

// Page exposes the underlying rod page for advanced flows.
func (s *BrowserSession) Page() *rod.Page { return s.page }

// RefreshLocation sets lastURL from the current browser tab (for Referer after redirects).
func (s *BrowserSession) RefreshLocation() error {
	if s.page == nil {
		return fmt.Errorf("crawler: no page")
	}
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "refresh_location", "")
	info, err := s.page.Info()
	if err != nil {
		return err
	}
	s.lastURL = info.URL
	logStepDone(log, "refresh_location", start, "url", info.URL)
	return nil
}

// Navigate loads urlStr, waits for load + network idle (best-effort for SPAs).
func (s *BrowserSession) Navigate(ctx context.Context, rawURL string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "navigate", "url", rawURL, "backend", string(BackendBrowser))
	s.cfg.emitPhase("navigating")

	pg := s.page.Timeout(s.cfg.NavigateTimeout)
	if err := pg.Navigate(rawURL); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}
	if err := pg.WaitLoad(); err != nil {
		return fmt.Errorf("wait load: %w", err)
	}
	if err := pg.WaitIdle(time.Minute); err != nil {
		log.Warn("crawler wait_idle", "err", err.Error())
	}

	s.lastURL = rawURL
	logStepDone(log, "navigate", start, "url", rawURL)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddNavigations(1)
	}
	return PauseBetweenSteps(ctx, s.cfg)
}

// NavigateLoadIdle loads urlStr, waits for load + network idle, without crawl pacing afterward.
func (s *BrowserSession) NavigateLoadIdle(ctx context.Context, rawURL string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "navigate_load_idle", "url", rawURL, "backend", string(BackendBrowser))

	pg := s.page.Timeout(s.cfg.NavigateTimeout)
	if err := pg.Navigate(rawURL); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}
	if err := pg.WaitLoad(); err != nil {
		return fmt.Errorf("wait load: %w", err)
	}
	if err := pg.WaitIdle(time.Minute); err != nil {
		log.Warn("crawler wait_idle", "err", err.Error())
	}
	s.lastURL = rawURL
	logStepDone(log, "navigate_load_idle", start, "url", rawURL)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddNavigations(1)
	}
	return nil
}

// ScrollBottom scrolls to document bottom (helps lazy-rendered lists).
func (s *BrowserSession) ScrollBottom(ctx context.Context) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "scroll", "target", "bottom")
	_, err := s.page.Eval(`() => { window.scrollTo(0, document.body.scrollHeight); }`)
	if err != nil {
		return err
	}
	logStepDone(log, "scroll", start)
	return PauseBetweenSteps(ctx, s.cfg)
}

// ScrollNestedContainers scrolls common SPA overflow containers (e.g. mat-sidenav-content) plus window.
func (s *BrowserSession) ScrollNestedContainers(ctx context.Context) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "scroll", "target", "nested_overflow")
	_, err := s.page.Eval(`() => {
		const nodes = document.querySelectorAll(
			'main, mat-sidenav-content, .mat-drawer-content, .main-content, [class*="overflow-auto"], .table-responsive'
		);
		nodes.forEach(el => {
			try {
				el.scrollTop = el.scrollHeight;
			} catch (e) {}
		});
		window.scrollTo(0, document.body.scrollHeight);
	}`)
	if err != nil {
		return err
	}
	logStepDone(log, "scroll", start)
	return PauseBetweenSteps(ctx, s.cfg)
}

// WaitVisible waits until selector matches at least one element.
func (s *BrowserSession) WaitVisible(ctx context.Context, selector string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "select", "selector", selector)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	if _, err := pg.Element(selector); err != nil {
		logStepDone(log, "select", start, "selector", selector, "found", false)
		return fmt.Errorf("%w: %q", ErrElementNotFound, selector)
	}
	logStepDone(log, "select", start, "selector", selector, "found", true)
	return PauseBetweenSteps(ctx, s.cfg)
}

// PollFirstXPath polls until any xpath matches an element or timeout elapses.
// Returns the index of the first matching xpath and true, or false if none matched.
func (s *BrowserSession) PollFirstXPath(ctx context.Context, xpaths []string, timeout time.Duration) (int, bool) {
	log := s.cfg.logger()
	if len(xpaths) == 0 || timeout <= 0 {
		return 0, false
	}
	log.Info("crawler", "step", "poll_xpath_begin", "candidates", len(xpaths), "timeout", timeout.String())
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return 0, false
		}
		if time.Now().After(deadline) {
			log.Info("crawler", "step", "poll_xpath_timeout", "candidates", len(xpaths))
			return 0, false
		}
		for i, xp := range xpaths {
			if xp == "" {
				continue
			}
			pg := s.page.Timeout(1500 * time.Millisecond)
			if _, err := pg.ElementX(xp); err == nil {
				log.Info("crawler", "step", "poll_xpath_hit", "index", i)
				return i, true
			}
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Click clicks the first element matching selector.
func (s *BrowserSession) Click(ctx context.Context, selector string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "click", "selector", selector)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	el, err := pg.Element(selector)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrElementNotFound, selector)
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click: %w", err)
	}
	logStepDone(log, "click", start, "selector", selector)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddClicks(1)
	}
	return PauseBetweenSteps(ctx, s.cfg)
}

// QuickClick clicks like Click but skips pacing (pair with WaitOpen / WaitDownload).
func (s *BrowserSession) QuickClick(ctx context.Context, selector string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "quick_click", "selector", selector)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	el, err := pg.Element(selector)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrElementNotFound, selector)
	}
	if err := el.ScrollIntoView(); err != nil {
		log.Warn("crawler quick_click scroll", "err", err.Error())
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("quick_click: %w", err)
	}
	logStepDone(log, "quick_click", start, "selector", selector)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddClicks(1)
	}
	return nil
}

// NavigateBack goes one step back in history and refreshes lastURL.
func (s *BrowserSession) NavigateBack(ctx context.Context) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "navigate_back", "")

	if err := s.page.NavigateBack(); err != nil {
		return fmt.Errorf("navigate back: %w", err)
	}
	if err := s.page.WaitLoad(); err != nil {
		log.Warn("crawler navigate_back wait_load", "err", err.Error())
	}
	if err := s.page.WaitIdle(45 * time.Second); err != nil {
		log.Warn("crawler navigate_back wait_idle", "err", err.Error())
	}
	if info, err := s.page.Info(); err == nil {
		s.lastURL = info.URL
	}
	logStepDone(log, "navigate_back", start)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddHistoryBacks(1)
	}
	return PauseBetweenSteps(ctx, s.cfg)
}

// EvalNumber evaluates a JS arrow function that returns a number (e.g. `() => 3`).
func (s *BrowserSession) EvalNumber(ctx context.Context, js string) (int64, error) {
	log := s.cfg.logger()
	start := time.Now()
	preview := js
	if len(preview) > 160 {
		preview = preview[:160] + "…"
	}
	logStep(log, "eval_number", "js", preview)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	res, err := pg.Eval(js)
	if err != nil {
		return 0, err
	}
	n := int64(res.Value.Int())
	logStepDone(log, "eval_number", start, "value", n)
	return n, nil
}

// EvalString evaluates a JS arrow function that returns a string (e.g. `() => document.title`).
func (s *BrowserSession) EvalString(ctx context.Context, js string) (string, error) {
	log := s.cfg.logger()
	start := time.Now()
	preview := js
	if len(preview) > 160 {
		preview = preview[:160] + "…"
	}
	logStep(log, "eval_string", "js", preview)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	res, err := pg.Eval(js)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Value.Str())
	logStepDone(log, "eval_string", start, "len", len(out))
	return out, nil
}

// CookieHeaderForURLs builds a Cookie header value for HTTP replay from the browser jar.
func (s *BrowserSession) CookieHeaderForURLs(urls []string) (string, error) {
	log := s.cfg.logger()
	start := time.Now()
	log.Info("crawler", "step", "cookie_header_begin", "url_count", len(urls))

	cks, err := s.page.Cookies(urls)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, c := range cks {
		parts = append(parts, c.Name+"="+c.Value)
	}
	out := strings.Join(parts, "; ")
	log.Info("crawler", "step", "cookie_header_done",
		"duration_ms", time.Since(start).Milliseconds(),
		"cookie_pairs", len(cks))
	return out, nil
}

// WaitPageLoadedIdle waits for the current tab's document load + network idle (best-effort SPA settle).
func (s *BrowserSession) WaitPageLoadedIdle(ctx context.Context, idleDeadline time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log := s.cfg.logger()
	log.Info("crawler", "step", "wait_page_loaded_idle", "idle_deadline", idleDeadline.String())

	pg := s.page.Timeout(s.cfg.NavigateTimeout)
	if err := pg.WaitLoad(); err != nil {
		return fmt.Errorf("wait load: %w", err)
	}
	if idleDeadline <= 0 {
		idleDeadline = time.Minute
	}
	if err := pg.WaitIdle(idleDeadline); err != nil {
		log.Warn("crawler_wait_idle", "err", err.Error())
	}
	return nil
}

func (s *BrowserSession) ClickXPath(ctx context.Context, xpath string) error {
	return s.clickXPath(ctx, xpath, true)
}

// ClickXPathQuick is ClickXPath without step pacing — pair with PollUntilVisible / SPA waits.
func (s *BrowserSession) ClickXPathQuick(ctx context.Context, xpath string) error {
	return s.clickXPath(ctx, xpath, false)
}

func (s *BrowserSession) clickXPath(ctx context.Context, xpath string, pauseAfter bool) error {
	log := s.cfg.logger()
	start := time.Now()
	step := "click_xpath"
	if !pauseAfter {
		step = "click_xpath_quick"
	}
	logStep(log, step, "xpath", xpath)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	el, err := pg.ElementX(xpath)
	if err != nil {
		logStepDone(log, step, start, "found", false)
		return fmt.Errorf("%w: xpath %q", ErrElementNotFound, xpath)
	}
	if err := el.ScrollIntoView(); err != nil {
		log.Warn("crawler click_xpath scroll", "err", err.Error())
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click xpath: %w", err)
	}
	logStepDone(log, step, start, "found", true)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddClicks(1)
	}
	if pauseAfter {
		return PauseBetweenSteps(ctx, s.cfg)
	}
	return nil
}

// ClickCollapseRadioByLabelSubstring finds a <label> whose text contains needle, then clicks its
// input.filter-collapse-bt (or any radio inside). Use when multiple collapse radios share classes.
func (s *BrowserSession) ClickCollapseRadioByLabelSubstring(ctx context.Context, needle string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "click_collapse_radio_js", "needle_len", len(needle))

	needleJSON, err := json.Marshal(needle)
	if err != nil {
		return err
	}
	js := fmt.Sprintf(`() => {
		const needle = %s;
		const labels = Array.from(document.querySelectorAll('label'));
		const lab = labels.find(l => {
			const t = (l.innerText || l.textContent || '').replace(/\s+/g, ' ').trim();
			return t.indexOf(needle) !== -1;
		});
		if (!lab) return 0;
		let inp = lab.querySelector('input.filter-collapse-bt[type="radio"]');
		if (!inp) inp = lab.querySelector('input[type="radio"].filter-collapse-bt');
		if (!inp) inp = lab.querySelector('input.filter-collapse-bt');
		if (!inp) inp = lab.querySelector('input[type="radio"]');
		if (inp) {
			inp.focus();
			inp.click();
			return 1;
		}
		lab.click();
		return 1;
	}`, string(needleJSON))

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	res, err := pg.Eval(js)
	if err != nil {
		return fmt.Errorf("eval collapse radio: %w", err)
	}
	if res.Value.Int() != 1 {
		logStepDone(log, "click_collapse_radio_js", start, "found", false)
		return fmt.Errorf("%w: label substring %q", ErrElementNotFound, needle)
	}
	logStepDone(log, "click_collapse_radio_js", start, "found", true)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddClicks(1)
	}
	return PauseBetweenSteps(ctx, s.cfg)
}

// Fill types text into the first input matching selector.
func (s *BrowserSession) Fill(ctx context.Context, selector, value string) error {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "fill", "selector", selector, "field", selector)

	pg := s.page.Timeout(s.cfg.ActionTimeout)
	el, err := pg.Element(selector)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrElementNotFound, selector)
	}
	if err := el.Input(value); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	logStepDone(log, "fill", start, "selector", selector)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddFormFills(1)
	}
	return PauseBetweenSteps(ctx, s.cfg)
}

// LinksHrefMatching returns absolute hrefs for elements matching cssSelector.
func (s *BrowserSession) LinksHrefMatching(ctx context.Context, cssSelector string) ([]string, error) {
	log := s.cfg.logger()
	start := time.Now()
	logStep(log, "extract_rows", "selector", cssSelector)

	info, err := s.page.Info()
	if err != nil {
		return nil, fmt.Errorf("page info: %w", err)
	}
	baseRef := info.URL
	if s.lastURL != "" {
		baseRef = s.lastURL
	}

	els, err := s.page.Timeout(s.cfg.ActionTimeout).Elements(cssSelector)
	if err != nil {
		return nil, err
	}

	var out []string
	for i, el := range els {
		hrefPtr, err := el.Attribute("href")
		if err != nil || hrefPtr == nil || strings.TrimSpace(*hrefPtr) == "" {
			continue
		}
		abs := resolveHref(strings.TrimSpace(*hrefPtr), baseRef)
		if abs != "" {
			out = append(out, abs)
		}
		if i%50 == 0 && i > 0 {
			log.Debug("crawler link scan progress", "count", i)
		}
	}
	logStepDone(log, "extract_rows", start, "selector", cssSelector, "count", len(out))
	if err := PauseBetweenSteps(ctx, s.cfg); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveHref(rawHref, baseURL string) string {
	rawHref = strings.TrimSpace(rawHref)
	if rawHref == "" || strings.HasPrefix(rawHref, "javascript:") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(rawHref)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

// DownloadURLs saves each HTTP(S) URL under cfg.DownloadDir using GET with Referer lastURL.
func (s *BrowserSession) DownloadURLs(ctx context.Context, hc *http.Client, urls []string) error {
	log := s.cfg.logger()
	ref := s.lastURL
	s.cfg.emitPhase("downloading")
	total := len(urls)
	for i, u := range urls {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		s.cfg.emitProgress(i+1, total)
		start := time.Now()
		path, n, err := HTTPDownload(ctx, s.cfg, hc, u, ref, "")
		if err != nil {
			log.Warn("crawler download failed", slog.String("url", u), slog.String("err", err.Error()))
			continue
		}
		if s.cfg.Stats != nil {
			s.cfg.Stats.AddDownloads(1)
		}
		if err := s.cfg.emitDownload(path); err != nil {
			return err
		}
		logStepDone(log, "download", start, "url", u, "dest_path", path, "bytes", n)
		if err := PauseBetweenSteps(ctx, s.cfg); err != nil {
			return err
		}
	}
	return nil
}

// WaitDownloadAfter registers for the next browser-mediated download, runs trigger (e.g. a click),
// waits for the download to finish, then moves the file from Chrome's GUID name into DownloadDir
// using UniqueFilename(suggested, url).
func (s *BrowserSession) WaitDownloadAfter(ctx context.Context, trigger func() error) (destPath string, err error) {
	log := s.cfg.logger()
	dir := s.cfg.DownloadDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir download dir: %w", err)
	}
	start := time.Now()
	log.Info("crawler", "step", "wait_download_begin", "dir", dir)

	waitDone := s.browser.WaitDownload(dir)
	if err := trigger(); err != nil {
		return "", fmt.Errorf("download trigger: %w", err)
	}

	type waitResult struct {
		info *proto.PageDownloadWillBegin
	}
	ch := make(chan waitResult, 1)
	go func() {
		ch <- waitResult{info: waitDone()}
	}()

	var info *proto.PageDownloadWillBegin
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case wr := <-ch:
		info = wr.info
	}
	if info == nil {
		return "", fmt.Errorf("crawler: download event missing")
	}
	src := filepath.Join(dir, info.GUID)
	base := strings.TrimSpace(info.SuggestedFilename)
	if base == "" {
		base = "download.pdf"
	}
	destPath = filepath.Join(dir, UniqueFilename(filepath.Base(base), info.URL))

	if err := os.Rename(src, destPath); err != nil {
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			return "", fmt.Errorf("finish download (rename/read src=%s): %w", src, rerr)
		}
		if werr := os.WriteFile(destPath, data, 0644); werr != nil {
			return "", werr
		}
		_ = os.Remove(src)
	}
	log.Info("crawler", "step", "wait_download_done",
		"duration_ms", time.Since(start).Milliseconds(),
		"dest_path", destPath,
		"suggested", info.SuggestedFilename)
	if s.cfg.Stats != nil {
		s.cfg.Stats.AddDownloads(1)
	}
	if err := s.cfg.emitDownload(destPath); err != nil {
		return "", err
	}
	return destPath, nil
}
