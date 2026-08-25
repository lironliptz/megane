package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// DoStep logs name then runs fn for imperative scripts. Errors are wrapped with name.
func (f *BrowserFlow) DoStep(ctx context.Context, name string, fn func(context.Context, *BrowserFlow) error) error {
	if f == nil {
		return fmt.Errorf("%s: nil BrowserFlow", name)
	}
	f.Logger().Info("crawler_step", "name", name)
	if err := fn(ctx, f); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// PollUntilVisible polls until cssSelector matches at least one element or totalTimeout elapses.
func (f *BrowserFlow) PollUntilVisible(ctx context.Context, cssSelector string, totalTimeout, pollEvery time.Duration) error {
	if cssSelector == "" {
		return fmt.Errorf("poll visible: empty selector")
	}
	if pollEvery <= 0 {
		pollEvery = 300 * time.Millisecond
	}
	log := f.Logger()
	log.Info("crawler_poll_visible", "selector", cssSelector, "timeout", totalTimeout.String())
	deadline := time.Now().Add(totalTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: poll visible %q", ErrElementNotFound, cssSelector)
		}
		pg := f.sess.page.Timeout(pollEvery)
		if _, err := pg.Element(cssSelector); err == nil {
			log.Info("crawler_poll_visible_hit", "selector", cssSelector)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// PollUntilAnyVisible returns the first css selector from candidates that becomes visible within totalTimeout.
func (f *BrowserFlow) PollUntilAnyVisible(ctx context.Context, candidates []string, totalTimeout, pollEvery time.Duration) (matchedSelector string, err error) {
	if pollEvery <= 0 {
		pollEvery = 300 * time.Millisecond
	}
	log := f.Logger()
	log.Info("crawler_poll_visible_any", "candidates", len(candidates), "timeout", totalTimeout.String())
	deadline := time.Now().Add(totalTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%w: poll any visible (%d selectors)", ErrElementNotFound, len(candidates))
		}
		for _, sel := range candidates {
			if sel == "" {
				continue
			}
			pg := f.sess.page.Timeout(pollEvery)
			if _, err := pg.Element(sel); err == nil {
				log.Info("crawler_poll_visible_hit", "selector", sel)
				return sel, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// TryClickXPathAfterPoll waits up to pollTotal for any xpath to match an element.
// When one matches it tries xpath clicks in hint order (seen index first). If nothing matched
// within pollTotal returns nil without error — useful for optional overlays (cookies, promos).
// If matched but every click fails, returns an error.
func (f *BrowserFlow) TryClickXPathAfterPoll(ctx context.Context, step string, xpaths []string, pollTotal, settleAfterClick time.Duration) error {
	if len(xpaths) == 0 {
		return nil
	}
	idx, ok := f.PollFirstXPath(ctx, xpaths, pollTotal)
	if !ok {
		f.Logger().Info("crawler_try_skip", "step", step)
		return nil
	}
	order := make([]string, 0, len(xpaths))
	order = append(order, xpaths[idx])
	for i, xp := range xpaths {
		if i != idx && xp != "" {
			order = append(order, xp)
		}
	}
	var lastErr error
	for _, xp := range order {
		if xp == "" {
			continue
		}
		err := f.ClickXPath(ctx, xp)
		if err == nil {
			f.Logger().Info("crawler_try_click_ok", "step", step, "xpath", xp)
			if settleAfterClick > 0 {
				return f.Sleep(ctx, settleAfterClick)
			}
			return nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return fmt.Errorf("%s: xpath matched but none clickable: %w", step, lastErr)
	}
	return fmt.Errorf("%s: xpath matched but none clickable", step)
}

// ClickXPathOneOf clicks the first xpath that succeeds; returns ErrElementNotFound if all fail.
func (f *BrowserFlow) ClickXPathOneOf(ctx context.Context, step string, xpaths []string) error {
	var lastErr error
	for _, xp := range xpaths {
		if xp == "" {
			continue
		}
		if err := f.ClickXPath(ctx, xp); err != nil {
			lastErr = err
			continue
		}
		f.Logger().Info("crawler_click_xpath_one_of", "step", step, "xpath_ok", xp)
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("%s: %w", step, lastErr)
	}
	return fmt.Errorf("%s: %w", step, ErrElementNotFound)
}

// DoubleBackSleepRefresh runs two history backs, optional sleepBetween before the second back,
// then sleepAfterFinal before refreshLocation (MAYA: use 0 between backs + ~2s after for “immediate” double back).
func (f *BrowserFlow) DoubleBackSleepRefresh(ctx context.Context, sleepBetween, sleepAfterFinal time.Duration, step string) error {
	log := f.Logger()
	log.Info("crawler_double_back", "step", step, "sleep_between", sleepBetween.String(), "sleep_after", sleepAfterFinal.String())
	if err := f.NavigateBack(ctx); err != nil {
		return fmt.Errorf("%s navigate back 1/2: %w", step, err)
	}
	if sleepBetween > 0 {
		if err := f.Sleep(ctx, sleepBetween); err != nil {
			return err
		}
	}
	if err := f.NavigateBack(ctx); err != nil {
		return fmt.Errorf("%s navigate back 2/2: %w", step, err)
	}
	if sleepAfterFinal > 0 {
		if err := f.Sleep(ctx, sleepAfterFinal); err != nil {
			return err
		}
	}
	if err := f.RefreshLocation(); err != nil {
		log.Warn("crawler_double_back_refresh", "step", step, "err", err.Error())
	}
	return nil
}

// TwoStepDownloadOpts configures “click → settle → click → capture download” (SPA navigation, popup, disk).
type TwoStepDownloadOpts struct {
	StepLabel    string       // slog context
	Logger       *slog.Logger // optional; defaults to flow logger
	PopupTimeout time.Duration

	// IdleAfterFirst, if positive, waits for load + network idle on the current tab after First
	// (prompr_maya_crawler: pdf 1 replaces the whole page — wait until that navigation settles).
	IdleAfterFirst time.Duration

	// AfterFirstWait is an optional extra pause after IdleAfterFirst / before Second.
	AfterFirstWait time.Duration

	First  func(context.Context, *BrowserFlow, *slog.Logger) error
	Second func(context.Context, *BrowserFlow, *slog.Logger) error
}

// DownloadAfterTwoStepClick runs First, waits for SPA settle (idle + optional sleep), then Second.
func (f *BrowserFlow) DownloadAfterTwoStepClick(ctx context.Context, hc *http.Client, opts TwoStepDownloadOpts, referer string, cookieURLs []string) (destPath string, err error) {
	log := opts.Logger
	if log == nil {
		log = f.Logger()
	}
	label := opts.StepLabel
	if label == "" {
		label = "two_step_download"
	}

	if opts.PopupTimeout <= 0 {
		opts.PopupTimeout = 45 * time.Second
	}
	legacySleepOnly := opts.IdleAfterFirst <= 0 && opts.AfterFirstWait <= 0
	if legacySleepOnly {
		opts.AfterFirstWait = time.Second
	}
	log.Info(label, "phase", "first_click_before")
	if opts.First != nil {
		if err := opts.First(ctx, f, log); err != nil {
			return "", fmt.Errorf("%s first click: %w", label, err)
		}
	}
	if opts.IdleAfterFirst > 0 {
		log.Info(label, "phase", "wait_loaded_idle_after_first", "idle_deadline", opts.IdleAfterFirst.String())
		if err := f.WaitPageLoadedIdle(ctx, opts.IdleAfterFirst); err != nil {
			return "", fmt.Errorf("%s after-first navigation settle: %w", label, err)
		}
	}
	if opts.AfterFirstWait > 0 {
		log.Info(label, "phase", "between_clicks_sleep", "duration", opts.AfterFirstWait.String())
		if err := f.Sleep(ctx, opts.AfterFirstWait); err != nil {
			return "", err
		}
	}

	pageURL, closePopup, errPop := f.WithNextPopup(ctx, opts.PopupTimeout, func() error {
		if opts.Second == nil {
			return fmt.Errorf("%s second click: nil handler", label)
		}
		return opts.Second(ctx, f, log)
	})
	if errPop == nil && closePopup != nil {
		if strings.HasPrefix(pageURL, "http") && !strings.Contains(strings.ToLower(pageURL), "blob:") {
			defer func() { _ = closePopup() }()
			urls := append(append([]string{}, cookieURLs...), pageURL)
			ck, err := f.CookieHeaderForURLs(urls)
			if err != nil {
				return "", fmt.Errorf("%s popup cookies: %w", label, err)
			}
			path, err := f.HTTPDownload(ctx, hc, pageURL, referer, ck)
			if err != nil {
				return "", err
			}
			log.Info(label, "phase", "popup_http_done", "path", path)
			return path, nil
		}
		log.Info(label, "phase", "popup_not_direct_http", "url", pageURL)
		_ = closePopup()
	}

	path, errDl := f.WaitDownloadAfter(ctx, func() error {
		if opts.Second == nil {
			return fmt.Errorf("%s second click: nil handler", label)
		}
		return opts.Second(ctx, f, log)
	})
	if errDl != nil {
		if errPop != nil {
			return "", fmt.Errorf("%s new-tab:%v disk:%w", label, errPop, errDl)
		}
		return "", fmt.Errorf("%s disk download: %w", label, errDl)
	}
	log.Info(label, "phase", "browser_download_done", "path", path)
	return path, nil
}
