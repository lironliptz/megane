package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// darwinHeadedChromeBins tries installed browsers first — rod's downloaded Chromium sometimes
// exits before CDP binds on macOS (EOF on /json/version), while Chrome.app is stable.
var darwinHeadedChromeBins = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
}

func chromeInstallCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return darwinHeadedChromeBins
	case "linux":
		return []string{
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/snap/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
		}
	default:
		return nil
	}
}

// InstalledChromeBin returns Google Chrome or Chromium at a well-known path if present (darwin/linux).
func InstalledChromeBin() string {
	for _, p := range chromeInstallCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func resolveChromeExecutable(cfg *Config) string {
	if cfg.ChromeBin != "" {
		return cfg.ChromeBin
	}
	// Use system Chrome/Chromium when present (stable for MAYA SPA); works headed or headless on darwin/linux.
	switch runtime.GOOS {
	case "darwin", "linux":
		if p := InstalledChromeBin(); p != "" {
			return p
		}
	}
	return ""
}

// LaunchBrowser starts a Chromium instance controlled by rod.
func LaunchBrowser(ctx context.Context, cfg *Config) (*rod.Browser, error) {
	l := launcher.New().
		Context(ctx).
		Headless(cfg.Headless).
		NoSandbox(runtime.GOOS == "linux"). // --no-sandbox crashes Chrome on macOS arm64
		Leakless(false)                     // leakless temp binary is blocked by macOS Gatekeeper; Close() is sufficient cleanup
	if bin := resolveChromeExecutable(cfg); bin != "" {
		l = l.Bin(bin)
		if cfg.ChromeBin == "" {
			cfg.logger().Info("crawler: using discovered Chrome/Chromium", "bin", bin, "headless", cfg.Headless, "goos", runtime.GOOS)
		}
	}
	if !cfg.Headless {
		// In visible mode Chrome 148+ exits immediately when no window is opened;
		// remove the rod default --no-startup-window so Chrome stays alive.
		l = l.Delete("no-startup-window")
	}
	if cfg.Devtools {
		l = l.Devtools(true)
	}

	// Bake the download dir into Chrome's Preferences so *all* downloads (including those
	// started by popup tabs or native navigation) land there — not in ~/Downloads.
	if cfg.DownloadDir != "" {
		if abs, err := filepath.Abs(cfg.DownloadDir); err == nil {
			if err := os.MkdirAll(abs, 0755); err == nil {
				prefs, _ := json.Marshal(map[string]any{
					"download": map[string]any{
						"default_directory":   abs,
						"prompt_for_download": false,
						"directory_upgrade":   true,
					},
					"plugins": map[string]any{
						"always_open_pdf_externally": true,
					},
				})
				l = l.Preferences(string(prefs))
			}
		}
	}

	controlURL, err := l.Launch()
	if err != nil {
		if !cfg.Headless {
			return nil, fmt.Errorf("launcher: %w (headed mode: need GUI access on this Mac; on darwin we auto-use Chrome.app when present — try -chrome-bin \"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome\", run from Terminal.app, or use -headless=true)", err)
		}
		return nil, fmt.Errorf("launcher: %w", err)
	}

	b := rod.New().ControlURL(controlURL).Context(ctx)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("rod connect: %w", err)
	}

	// Also set via CDP so rod's WaitDownload and any popup targets inherit the same dir.
	if cfg.DownloadDir != "" {
		if abs, err := filepath.Abs(cfg.DownloadDir); err == nil {
			_ = proto.BrowserSetDownloadBehavior{
				Behavior:      proto.BrowserSetDownloadBehaviorBehaviorAllow,
				DownloadPath:  abs,
				EventsEnabled: true,
			}.Call(b)
		}
	}

	return b, nil
}
