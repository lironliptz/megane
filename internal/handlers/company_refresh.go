package handlers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"megane/internal/companyview"
	"megane/internal/filedb"
)

type refreshStatus struct {
	CIK        string `json:"cik"`
	Status     string `json:"status"` // idle|running|complete|error
	Label      string `json:"label"`
	Progress   int    `json:"progress"`
	InProgress bool   `json:"in_progress"`
	LastError  string `json:"last_error,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type refreshManager struct {
	mu      sync.Mutex
	states  map[string]refreshStatus
	running map[string]bool
}

func newRefreshManager() *refreshManager {
	return &refreshManager{
		states:  map[string]refreshStatus{},
		running: map[string]bool{},
	}
}

func (m *refreshManager) get(cik string) refreshStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.states[cik]; ok {
		return st
	}
	return refreshStatus{CIK: cik, Status: "idle", Label: "Not started", Progress: 0, InProgress: false}
}

func (m *refreshManager) start(cik string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running[cik] {
		return false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	m.running[cik] = true
	m.states[cik] = refreshStatus{
		CIK:        cik,
		Status:     "running",
		Label:      "Queued",
		Progress:   5,
		InProgress: true,
		StartedAt:  now,
		UpdatedAt:  now,
	}
	return true
}

func (m *refreshManager) update(cik, label string, progress int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[cik]
	st.Status = "running"
	st.Label = label
	st.Progress = progress
	st.InProgress = true
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.states[cik] = st
}

func (m *refreshManager) fail(cik string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[cik]
	st.Status = "error"
	st.Label = "Refresh failed"
	st.Progress = 100
	st.InProgress = false
	st.LastError = err.Error()
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.states[cik] = st
	delete(m.running, cik)
}

func (m *refreshManager) done(cik string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[cik]
	st.Status = "complete"
	st.Label = "Refresh complete"
	st.Progress = 100
	st.InProgress = false
	st.LastError = ""
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.states[cik] = st
	delete(m.running, cik)
}

func runRefreshPipeline(ctx context.Context, timeline *companyview.Service, store filedb.CompanyStore, cik string, m *refreshManager) error {
	detail, err := store.GetCompany(ctx, cik)
	if err != nil {
		return err
	}
	ticker := ""
	if len(detail.Identity.Tickers) > 0 {
		ticker = detail.Identity.Tickers[0]
	}

	root := strings.TrimSpace(os.Getenv("FILEDB_DIR"))
	if root == "" {
		root = "./fileDB"
	}
	years := strings.TrimSpace(os.Getenv("FETCH_UPDATED_YEARS"))
	if years == "" {
		years = "10"
	}
	ua := strings.TrimSpace(os.Getenv("SEC_EDGAR_USER_AGENT"))
	if ua == "" {
		ua = "MyResearchApp your-email@example.com"
	}

	companyDir := filepath.Join(root, "companies", cik)

	m.update(cik, "Fetching latest SEC filings", 10)
	if err := runCmd(ctx, cik, ".", "python3",
		func(line string) {
			stage, done, total, ok := parseProgressLine(line)
			if !ok || stage != "sec" {
				return
			}
			m.update(cik, fmt.Sprintf("Fetching latest SEC filings (%d/%d)", done, total), weightedProgress(10, 45, done, total))
		},
		"python/edgar/fetch_kamada.py", "--cik", cik, "--years", years, "--root", root, "--user-agent", ua); err != nil {
		return err
	}

	m.update(cik, "Building filing metadata", 55)
	if err := runCmd(ctx, cik, ".", "python3",
		func(line string) {
			stage, done, total, ok := parseProgressLine(line)
			if !ok || stage != "meta" {
				return
			}
			m.update(cik, fmt.Sprintf("Building filing metadata (%d/%d)", done, total), weightedProgress(55, 20, done, total))
		},
		"python/edgar/build_meta_all.py", companyDir); err != nil {
		return err
	}

	m.update(cik, "Extracting and gap-filling financials", 75)
	if err := runCmd(ctx, cik, ".", "go", nil,
		"run", "./cmd/edgar-financials", "--root", root, "--cik", cik, "--fill-gaps", "--all"); err != nil {
		return err
	}
	m.update(cik, "Extracting and gap-filling financials", 85)

	if timeline != nil {
		m.update(cik, "Refreshing stock prices & volume", 88)
		fmt.Printf("company-refresh cik=%s stage=prices start\n", cik)
		if err := timeline.RefreshCompanyData(ctx, cik); err != nil {
			return err
		}
		fmt.Printf("company-refresh cik=%s stage=prices done\n", cik)
		m.update(cik, "Refreshing stock prices & volume", 95)
	}

	if strings.TrimSpace(os.Getenv("FINNHUB_API_KEY")) != "" {
		m.update(cik, "Refreshing consensus data", 97)
		args := []string{"run", "./cmd/collect-consensus", "--cik", cik}
		if ticker != "" {
			args = append(args, "--ticker", ticker)
		}
		args = append(args, "--snapshot")
		if err := runCmd(ctx, cik, ".", "go", nil, args...); err != nil {
			return err
		}
	}
	return nil
}

func runCmd(ctx context.Context, cik, dir, name string, onLine func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	fmt.Printf("company-refresh cik=%s cmd=%s args=%s start\n", cik, name, strings.Join(args, " "))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	lines := make([]string, 0, 32)
	var linesMu sync.Mutex
	recordLine := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		fmt.Printf("company-refresh cik=%s cmd=%s line=%s\n", cik, name, line)
		if onLine != nil {
			onLine(line)
		}
		linesMu.Lock()
		lines = append(lines, line)
		if len(lines) > 40 {
			lines = lines[len(lines)-40:]
		}
		linesMu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanPipe(&wg, stdout, recordLine)
	go scanPipe(&wg, stderr, recordLine)
	wg.Wait()
	err = cmd.Wait()
	if err != nil {
		linesMu.Lock()
		msg := strings.Join(lines, " | ")
		linesMu.Unlock()
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), msg)
	}
	fmt.Printf("company-refresh cik=%s cmd=%s done\n", cik, name)
	return nil
}

func scanPipe(wg *sync.WaitGroup, r io.ReadCloser, onLine func(string)) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		onLine(sc.Text())
	}
}

func parseProgressLine(line string) (stage string, done, total int, ok bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) != 4 || parts[0] != "MEGANE_PROGRESS" {
		return "", 0, 0, false
	}
	doneN, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, 0, false
	}
	totalN, err := strconv.Atoi(parts[3])
	if err != nil || totalN <= 0 {
		return "", 0, 0, false
	}
	if doneN < 0 {
		doneN = 0
	}
	if doneN > totalN {
		doneN = totalN
	}
	return parts[1], doneN, totalN, true
}

func weightedProgress(base, span, done, total int) int {
	if total <= 0 {
		return base
	}
	pct := base + (span*done)/total
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
