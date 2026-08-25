package crawler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// autocompleteProbeTimeout caps how long we wait per candidate input selector before trying the
// next one. Without this, the first stale/wrong id can block for the full ActionTimeout (~minutes).
const autocompleteProbeTimeout = 5 * time.Second

// MaterialAutocompletePickFirst fills the first matching input in inputSelectors with query,
// waits for a Material autocomplete panel, and clicks the first option.
func (f *BrowserFlow) MaterialAutocompletePickFirst(ctx context.Context, inputSelectors []string, query string) error {
	log := f.sess.cfg.logger()
	logStep(log, "material_autocomplete", "query_len", len(query))

	var filled bool
	for _, sel := range inputSelectors {
		if sel == "" {
			continue
		}
		pg := f.sess.page.Timeout(autocompleteProbeTimeout)
		el, err := pg.Element(sel)
		if err != nil {
			log.Info("crawler", "step", "material_autocomplete_probe_miss", "selector", sel)
			continue
		}
		if err := el.ScrollIntoView(); err != nil {
			log.Warn("crawler autocomplete scroll_into_view", "err", err.Error())
		}
		if err := el.Focus(); err != nil {
			log.Warn("crawler autocomplete focus", "err", err.Error())
		}
		if err := el.SelectAllText(); err == nil {
			_ = el.Input("")
		}
		if err := el.Input(query); err != nil {
			continue
		}
		filled = true
		log.Info("crawler", "step", "material_autocomplete_filled", "selector", sel, "value_chars", len(query))
		logStep(log, "material_autocomplete", "selector_used", sel)
		break
	}
	if !filled {
		return fmt.Errorf("material autocomplete: could not type into input (tried %d selectors)", len(inputSelectors))
	}

	log.Info("crawler", "step", "material_autocomplete_wait_panel", "after_sleep_ms", 700)
	if err := f.Sleep(ctx, 700*time.Millisecond); err != nil {
		return err
	}
	if err := PauseBetweenSteps(ctx, f.sess.cfg); err != nil {
		return err
	}

	pg := f.sess.page.Timeout(f.sess.cfg.ActionTimeout)
	if _, err := pg.Element(`.mat-mdc-autocomplete-panel`); err != nil {
		return fmt.Errorf("material autocomplete: panel not visible: %w", err)
	}
	log.Info("crawler", "step", "material_autocomplete_click_first_option")

	opts, err := pg.Elements(`.mat-mdc-autocomplete-panel mat-option[role="option"]`)
	if err != nil || len(opts) == 0 {
		opts, err = pg.Elements(`mat-option[role="option"]`)
	}
	if err != nil || len(opts) == 0 {
		return fmt.Errorf("%w: autocomplete options", ErrElementNotFound)
	}
	if err := opts[0].ScrollIntoView(); err != nil {
		log.Warn("crawler option scroll", "err", err.Error())
	}
	if err := opts[0].Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click first autocomplete option: %w", err)
	}
	return PauseBetweenSteps(ctx, f.sess.cfg)
}

// MaterialSelectYear opens the Material select that shows a 4-digit year and chooses an
// option whose visible text contains year (e.g. "2023").
func (f *BrowserFlow) MaterialSelectYear(ctx context.Context, year string) error {
	log := f.sess.cfg.logger()
	year = strings.TrimSpace(year)
	if year == "" {
		return fmt.Errorf("year empty")
	}
	logStep(log, "material_select_year", "year", year)

	pg := f.sess.page.Timeout(f.sess.cfg.ActionTimeout)
	_, err := pg.Eval(`() => {
		const spans = Array.from(document.querySelectorAll('.mat-mdc-select-min-line'));
		const y = spans.find(s => /^\d{4}$/.test((s.textContent || '').trim()));
		if (!y) return false;
		const ms = y.closest('mat-select');
		const tr = ms && ms.querySelector('.mat-mdc-select-trigger');
		if (tr) { tr.click(); return true; }
		return false;
	}`)
	if err != nil {
		return fmt.Errorf("open year mat-select: %w", err)
	}
	log.Info("crawler", "step", "material_select_year_opened_dropdown")

	if err := f.Sleep(ctx, 400*time.Millisecond); err != nil {
		return err
	}

	pg = f.sess.page.Timeout(f.sess.cfg.ActionTimeout)
	opts, err := pg.Elements(`.cdk-overlay-container mat-option[role="option"], .mat-mdc-select-panel mat-option`)
	if err != nil || len(opts) == 0 {
		opts, err = pg.Elements(`mat-option[role="option"]`)
	}
	if err != nil || len(opts) == 0 {
		return fmt.Errorf("%w: year mat-option", ErrElementNotFound)
	}

	for _, o := range opts {
		txt, err := o.Text()
		if err != nil {
			continue
		}
		if strings.Contains(strings.TrimSpace(txt), year) {
			log.Info("crawler", "step", "material_select_year_pick", "year", year, "option_text", strings.TrimSpace(txt))
			if err := o.ScrollIntoView(); err != nil {
				log.Warn("year option scroll", "err", err.Error())
			}
			return o.Click(proto.InputMouseButtonLeft, 1)
		}
	}
	return fmt.Errorf("no mat-option containing year %q", year)
}
