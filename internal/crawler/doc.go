// Package crawler provides a small imperative toolkit for fetching web pages:
// Colly for static HTML and go-rod [https://github.com/go-rod/rod] when JavaScript is required.
//
// Requires a Chromium-based browser on PATH for the browser backend (rod downloads one by default
// on first use if configured). For debugging SPAs, set Config.Headless=false and optionally
// Config.Devtools=true so the window and Chrome DevTools stay visible.
//
// Choosing a backend
//
// Use [NewStaticClient] and [StaticClient.FetchHTML] when the response is usable without executing
// client-side JavaScript. Use [OpenBrowserFlow], [BrowserFlow.Navigate], and related methods when
// the site renders or navigates via JS (SPAs, deferred content); [BrowserFlow] hides rod from app
// packages. Lower-level [OpenBrowserSession] remains available when direct rod access is needed.
//
// Constants [BackendStatic] and [BackendBrowser] label logs; there is no single factory—callers
// pick the API matching their scenario (simple HTTP vs browser session).
//
// Legal use: comply with each site's terms of service and applicable law. Respect robots.txt
// where appropriate (no built-in fetch/enforce helper yet—callers may validate before crawling).
// This library does not bypass protections or CAPTCHAs.
package crawler
