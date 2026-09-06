package consensus

import (
	"context"
	"time"

	"megane/internal/models"
)

// NasdaqAPI is a keyless recent-quarter fallback source (P1).
type NasdaqAPI struct{}

func NewNasdaqAPI() *NasdaqAPI { return &NasdaqAPI{} }

func (n *NasdaqAPI) Slug() string { return SourceNasdaq }

func (n *NasdaqAPI) FetchEarnings(_ context.Context, _ string, _ time.Time, _ time.Time) ([]models.ConsensusPeriod, error) {
	return nil, ErrNoData
}

func (n *NasdaqAPI) FetchSnapshot(_ context.Context, _ string) (*models.ConsensusSnapshot, error) {
	return nil, ErrNoData
}
