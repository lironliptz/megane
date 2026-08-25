package crawler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CookieHeaderForURLs builds a Cookie header for HTTP replay from the browser jar (see BrowserSession).
func (f *BrowserFlow) CookieHeaderForURLs(urls []string) (string, error) {
	return f.sess.CookieHeaderForURLs(urls)
}

// WithNextPopup registers for the next page opened from this tab, runs trigger (typically a click),
// then resolves the popup URL. Close the popup by calling closePopup when done.
func (f *BrowserFlow) WithNextPopup(ctx context.Context, popupTimeout time.Duration, trigger func() error) (pageURL string, closePopup func() error, err error) {
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}
	log := f.sess.cfg.logger()
	pg := f.sess.page.Timeout(popupTimeout)
	wait := pg.WaitOpen()

	if err := trigger(); err != nil {
		return "", nil, err
	}

	log.Info("crawler", "step", "popup_wait_resolve")
	pop, popErr := wait()
	if popErr != nil {
		// The click may have navigated the current tab (e.g. Angular router) rather than
		// opening a new popup.  f.sess.page still refers to the tab — check its new URL.
		if info2, err2 := f.sess.page.Info(); err2 == nil {
			u2 := strings.TrimSpace(info2.URL)
			log.Info("crawler", "step", "popup_in_tab_nav_fallback", "url", u2, "popup_err", popErr.Error())
			if strings.HasPrefix(u2, "http") {
				// Caller must call NavigateBack; closePopup is a no-op for in-tab case.
				return u2, func() error { return nil }, nil
			}
		}
		return "", nil, fmt.Errorf("popup wait: %w", popErr)
	}
	if pop == nil {
		return "", nil, fmt.Errorf("popup: nil page")
	}
	closePopup = func() error {
		return pop.Close()
	}
	if err := pop.WaitLoad(); err != nil {
		log.Warn("crawler popup wait_load", "err", err.Error())
	}
	info, err := pop.Info()
	if err != nil {
		_ = pop.Close()
		return "", nil, fmt.Errorf("popup info: %w", err)
	}
	u := strings.TrimSpace(info.URL)
	return u, closePopup, nil
}
