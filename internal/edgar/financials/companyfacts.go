package financials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	companyFactsURL  = "https://data.sec.gov/api/xbrl/companyfacts/CIK%s.json"
	companyFactsDir  = ".sec"
	companyFactsFile = "companyfacts.json"
	// defaultCFTTL keeps a company's aggregate document for a week. Restatements
	// are rare; refetching per run would be rude to a free public service.
	defaultCFTTL = 7 * 24 * time.Hour
	// cfTimeout is generous: the measured document for one mid-cap issuer is
	// 1.1 MB and arrives in about a second, but issuers with long histories are
	// substantially larger.
	cfTimeout = 30 * time.Second
)

var (
	// ErrNoUserAgent is returned before any request is attempted. data.sec.gov
	// rejects anonymous clients, and inventing a contact address would
	// misrepresent the caller to SEC, so this fails fast instead.
	ErrNoUserAgent = errors.New("financials: SEC_EDGAR_USER_AGENT is required for SEC requests")
	// ErrRateLimited signals SEC's fair-access throttle.
	ErrRateLimited = errors.New("financials: SEC rate-limited the request")
	// ErrNoCompanyFacts means SEC holds no aggregate facts for the CIK.
	ErrNoCompanyFacts = errors.New("financials: no Company Facts for CIK")
)

// Client fetches and caches SEC Company Facts documents.
//
// Shaped after internal/marketdata's providers: an injectable *http.Client, an
// explicit UserAgent, and sentinel errors so callers can distinguish a throttle
// from a genuine absence.
type Client struct {
	BaseURL   string // defaults to SEC; overridden by tests
	HTTP      *http.Client
	UserAgent string
	TTL       time.Duration
}

// NewClient builds a client from the environment.
func NewClient(userAgent string, ttl time.Duration) *Client {
	if ttl <= 0 {
		ttl = defaultCFTTL
	}
	return &Client{
		BaseURL:   companyFactsURL,
		HTTP:      &http.Client{Timeout: cfTimeout},
		UserAgent: strings.TrimSpace(userAgent),
		TTL:       ttl,
	}
}

// FactEntry is one reported value for one concept and period.
//
// Start is absent for instants (balance-sheet concepts). There is deliberately
// no Decimals field: Company Facts does not carry one, which is why scale is
// resolved by cross-accession comparison rather than by inspecting a tag.
type FactEntry struct {
	Start string  `json:"start,omitempty"`
	End   string  `json:"end"`
	Val   float64 `json:"val"`
	Accn  string  `json:"accn"`
	FY    int     `json:"fy"`
	FP    string  `json:"fp"`
	Form  string  `json:"form"`
	Filed string  `json:"filed"`
}

type conceptFacts struct {
	Units map[string][]FactEntry `json:"units"`
}

// CompanyFacts is the whole aggregate document: namespace -> concept -> units.
type CompanyFacts struct {
	CIK        int                                `json:"cik"`
	EntityName string                             `json:"entityName"`
	Facts      map[string]map[string]conceptFacts `json:"facts"`
	// FetchedAt is set by the client, not by SEC, and is recorded on every line
	// this document produces.
	FetchedAt time.Time `json:"-"`
}

// PadCIK renders a CIK in the zero-padded 10-digit form the API expects.
func PadCIK(cik string) string {
	cik = strings.TrimSpace(cik)
	for len(cik) < 10 {
		cik = "0" + cik
	}
	return cik
}

// CachePath is where a company's aggregate document is stored. It sits inside
// the corpus directory, which is gitignored, so the cache is never committed.
func CachePath(root, cik string) string {
	return filepath.Join(root, companiesDirName, cik, companyFactsDir, companyFactsFile)
}

const companiesDirName = "companies"

// Facts returns the company's aggregate facts, from cache when fresh.
//
// One call serves every accession of a company: callers must not invoke this
// per accession.
func (c *Client) Facts(ctx context.Context, root, cik string, force bool) (*CompanyFacts, error) {
	path := CachePath(root, cik)
	if !force {
		if cf, ok := c.readCache(path); ok {
			return cf, nil
		}
	}
	body, err := c.get(ctx, cik)
	if err != nil {
		// A stale cache beats no data when SEC is unreachable or throttling.
		if cf, ok := c.readCacheAnyAge(path); ok {
			return cf, nil
		}
		return nil, err
	}
	if err := writeAtomic(path, body); err != nil {
		return nil, err
	}
	return decodeFacts(body, time.Now().UTC())
}

func (c *Client) readCache(path string) (*CompanyFacts, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = defaultCFTTL
	}
	if time.Since(fi.ModTime()) > ttl {
		return nil, false
	}
	return c.readCacheAnyAge(path)
}

func (c *Client) readCacheAnyAge(path string) (*CompanyFacts, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	fi, err := os.Stat(path)
	at := time.Now().UTC()
	if err == nil {
		at = fi.ModTime().UTC()
	}
	cf, err := decodeFacts(b, at)
	if err != nil {
		return nil, false
	}
	return cf, true
}

func (c *Client) get(ctx context.Context, cik string) ([]byte, error) {
	if c.UserAgent == "" {
		return nil, ErrNoUserAgent
	}
	base := c.BaseURL
	if base == "" {
		base = companyFactsURL
	}
	url := base
	if strings.Contains(base, "%s") {
		url = fmt.Sprintf(base, PadCIK(cik))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: cfTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("financials: fetch company facts: %w", err)
	}
	defer resp.Body.Close()

	// Check status before reading: a throttle response is not guaranteed JSON.
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNoCompanyFacts
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("financials: company facts HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func decodeFacts(b []byte, at time.Time) (*CompanyFacts, error) {
	var cf CompanyFacts
	if err := json.Unmarshal(b, &cf); err != nil {
		return nil, fmt.Errorf("financials: parse company facts: %w", err)
	}
	if len(cf.Facts) == 0 {
		return nil, ErrNoCompanyFacts
	}
	cf.FetchedAt = at.Truncate(time.Second)
	return &cf, nil
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
