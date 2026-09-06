package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"megane/internal/models"
)

type Finnhub struct {
	apiKey string
	base   string
	client *http.Client
}

func NewFinnhub(apiKey string, client *http.Client) (*Finnhub, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, ErrNoAPIKey
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Finnhub{
		apiKey: strings.TrimSpace(apiKey),
		base:   "https://finnhub.io/api/v1",
		client: client,
	}, nil
}

func (f *Finnhub) Slug() string { return SourceFinnhub }

func (f *Finnhub) FetchEarnings(ctx context.Context, ticker string, from, to time.Time) ([]models.ConsensusPeriod, error) {
	var payload []struct {
		Actual          *float64 `json:"actual"`
		Estimate        *float64 `json:"estimate"`
		Surprise        *float64 `json:"surprise"`
		SurprisePercent *float64 `json:"surprisePercent"`
		Period          string   `json:"period"`
		Year            int      `json:"year"`
		Quarter         int      `json:"quarter"`
	}
	if err := f.getJSON(ctx, "/stock/earnings", map[string]string{"symbol": strings.TrimSpace(ticker)}, &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, ErrNoData
	}

	out := make([]models.ConsensusPeriod, 0, len(payload))
	now := time.Now().UTC()
	for _, row := range payload {
		fiscalPeriod := deriveQuarterLabel(row.Year, row.Quarter)
		if row.Quarter == 0 {
			fiscalPeriod = "FY"
		}
		if fiscalPeriod == "" {
			continue
		}

		periodEnd := row.Period
		if _, err := time.Parse("2006-01-02", periodEnd); err != nil {
			periodEnd = ""
		}
		duration := "P3M"
		if fiscalPeriod == "FY" {
			duration = "P1Y"
		}
		fiscalYear := strconv.Itoa(row.Year)

		consensusDate := periodEnd
		if consensusDate == "" {
			consensusDate = fmt.Sprintf("%04d-12-31", row.Year)
		}
		if fromStr := from.Format("2006-01-02"); consensusDate < fromStr {
			continue
		}
		if toStr := to.Format("2006-01-02"); consensusDate > toStr {
			continue
		}

		out = append(out, models.ConsensusPeriod{
			Source:         SourceFinnhub,
			FiscalYear:     fiscalYear,
			FiscalPeriod:   fiscalPeriod,
			PeriodEnd:      periodEnd,
			Duration:       duration,
			ConsensusDate:  consensusDate,
			EPSEstimate:    row.Estimate,
			EPSActual:      row.Actual,
			EPSSurprise:    row.Surprise,
			EPSSurprisePct: row.SurprisePercent,
			Basis:          "unknown",
			Currency:       "USD",
			FetchedAt:      now,
		})
	}
	if len(out) == 0 {
		return nil, ErrNoData
	}
	return out, nil
}

func (f *Finnhub) FetchSnapshot(ctx context.Context, ticker string) (*models.ConsensusSnapshot, error) {
	var pt struct {
		TargetHigh   *float64 `json:"targetHigh"`
		TargetLow    *float64 `json:"targetLow"`
		TargetMean   *float64 `json:"targetMean"`
		TargetMedian *float64 `json:"targetMedian"`
	}
	if err := f.getJSON(ctx, "/stock/price-target", map[string]string{"symbol": strings.TrimSpace(ticker)}, &pt); err != nil {
		return nil, err
	}

	var rec []struct {
		Buy        int    `json:"buy"`
		Hold       int    `json:"hold"`
		Sell       int    `json:"sell"`
		StrongBuy  int    `json:"strongBuy"`
		StrongSell int    `json:"strongSell"`
		Period     string `json:"period"`
	}
	if err := f.getJSON(ctx, "/stock/recommendation", map[string]string{"symbol": strings.TrimSpace(ticker)}, &rec); err != nil {
		return nil, err
	}

	s := &models.ConsensusSnapshot{
		Source:    SourceFinnhub,
		AsOf:      time.Now().UTC().Format("2006-01-02"),
		PTMean:    pt.TargetMean,
		PTHigh:    pt.TargetHigh,
		PTLow:     pt.TargetLow,
		PTMedian:  pt.TargetMedian,
		Currency:  "USD",
		FetchedAt: time.Now().UTC(),
	}
	if len(rec) > 0 {
		r := rec[0]
		s.Buy = r.Buy + r.StrongBuy
		s.Hold = r.Hold
		s.Sell = r.Sell + r.StrongSell
		total := s.Buy + s.Hold + s.Sell
		s.AnalystCount = &total
	}
	return s, nil
}

func (f *Finnhub) getJSON(ctx context.Context, endpoint string, query map[string]string, out any) error {
	if strings.TrimSpace(f.apiKey) == "" {
		return ErrNoAPIKey
	}
	u, _ := url.Parse(f.base + endpoint)
	q := u.Query()
	q.Set("token", f.apiKey)
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("finnhub request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimit
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("finnhub status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("finnhub decode: %w", err)
	}
	return nil
}
