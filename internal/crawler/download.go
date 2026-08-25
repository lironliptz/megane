package crawler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeFilename = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// UniqueFilename builds "{unixNano}_{8hexsha}_{sanitized_base}".
func UniqueFilename(originalBasename, urlStr string) string {
	h := sha256.Sum256([]byte(urlStr))
	short := hex.EncodeToString(h[:4])
	base := filepath.Base(originalBasename)
	if base == "." || base == "/" || base == "" {
		base = "download"
	}
	base = unsafeFilename.ReplaceAllString(base, "_")
	if len(base) > 120 {
		base = base[:120]
	}
	return fmt.Sprintf("%d_%s_%s", time.Now().UnixNano(), short, base)
}

// SaveReader writes r to DownloadDir using UniqueFilename(suggestedName, urlStr).
func SaveReader(ctx context.Context, cfg *Config, r io.Reader, suggestedName, urlStr string) (destPath string, n int64, err error) {
	name := UniqueFilename(suggestedName, urlStr)
	destPath = filepath.Join(cfg.DownloadDir, name)
	if err := os.MkdirAll(cfg.DownloadDir, 0755); err != nil {
		return "", 0, fmt.Errorf("mkdir download dir: %w", err)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return "", 0, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	n, err = io.Copy(f, r)
	if err != nil {
		_ = os.Remove(destPath)
		return "", 0, fmt.Errorf("write body: %w", err)
	}
	return destPath, n, nil
}

// HTTPDownload performs GET urlStr and saves the body using cfg.DownloadDir.
// cookieHeader is optional (e.g. document.cookie-style "a=b; c=d" for authenticated GETs).
func HTTPDownload(ctx context.Context, cfg *Config, client *http.Client, urlStr string, referer string, cookieHeader string) (destPath string, n int64, err error) {
	log := cfg.logger()
	start := time.Now()
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", 0, err
	}
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	if cfg.UserAgent != "" {
		req.Header.Set("User-Agent", cfg.UserAgent)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("http status %d", resp.StatusCode)
	}
	base := "download"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mimeParseContentDisposition(cd); err == nil && params["filename"] != "" {
			base = params["filename"]
		}
	}
	if u, err := urlpkg.Parse(urlStr); err == nil && u.Path != "" {
		if base == "download" || !strings.Contains(base, ".") {
			base = filepath.Base(u.Path)
		}
	}
	path, n, err := SaveReader(ctx, cfg, resp.Body, base, urlStr)
	ct := resp.Header.Get("Content-Type")
	logStepDone(log, "download", start, "url", urlStr, "dest_path", path, "bytes", n, "content_type", ct)
	return path, n, err
}

// minimal Content-Disposition filename parser (attachment; filename="x.pdf")
func mimeParseContentDisposition(h string) (string, map[string]string, error) {
	params := map[string]string{}
	semi := strings.Split(h, ";")
	if len(semi) == 0 {
		return "", params, fmt.Errorf("empty")
	}
	for _, part := range semi[1:] {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			k := strings.TrimSpace(strings.ToLower(kv[0]))
			v := strings.Trim(strings.TrimSpace(kv[1]), `"`)
			params[k] = v
		}
	}
	return strings.TrimSpace(semi[0]), params, nil
}
