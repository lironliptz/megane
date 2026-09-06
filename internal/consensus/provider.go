package consensus

import (
	"context"
	"errors"
	"time"

	"megane/internal/models"
)

const (
	SourceFinnhub      = "finnhub"
	SourceFMP          = "fmp"
	SourceNasdaq       = "nasdaq_api"
	SourceAlphaVantage = "alpha_vantage"
)

var (
	ErrNoAPIKey  = errors.New("consensus: vendor API key not configured")
	ErrRateLimit = errors.New("consensus: vendor rate-limited the request")
	ErrNoData    = errors.New("consensus: vendor has no data for this symbol")
)

type Provider interface {
	Slug() string
	FetchEarnings(ctx context.Context, ticker string, from, to time.Time) ([]models.ConsensusPeriod, error)
	FetchSnapshot(ctx context.Context, ticker string) (*models.ConsensusSnapshot, error)
}
