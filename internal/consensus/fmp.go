package consensus

import (
	"context"
	"strings"
	"time"

	"megane/internal/models"
)

// FMP is reserved for revenue-estimate enrichment (P1).
type FMP struct {
	apiKey string
}

func NewFMP(apiKey string) (*FMP, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, ErrNoAPIKey
	}
	return &FMP{apiKey: strings.TrimSpace(apiKey)}, nil
}

func (f *FMP) Slug() string { return SourceFMP }

func (f *FMP) FetchEarnings(_ context.Context, _ string, _ time.Time, _ time.Time) ([]models.ConsensusPeriod, error) {
	return nil, ErrNoData
}

func (f *FMP) FetchSnapshot(_ context.Context, _ string) (*models.ConsensusSnapshot, error) {
	return nil, ErrNoData
}
