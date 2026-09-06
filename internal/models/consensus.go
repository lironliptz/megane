package models

import "time"

// ConsensusPeriod is one point-in-time analyst consensus row for a fiscal period.
type ConsensusPeriod struct {
	CIK              string
	Ticker           string
	Source           string
	SourceURL        string
	FiscalYear       string
	FiscalPeriod     string
	PeriodEnd        string
	Duration         string
	ConsensusDate    string
	AnnouncementDate string
	EPSEstimate      *float64
	EPSActual        *float64
	EPSSurprise      *float64
	EPSSurprisePct   *float64
	RevenueEstimate  *float64
	Basis            string
	Currency         string
	RatingBuyCount   int
	RatingHoldCount  int
	RatingSellCount  int
	FetchedAt        time.Time
}

// ConsensusSnapshot is a non-periodic snapshot (price target / ratings) for a day.
type ConsensusSnapshot struct {
	CIK          string
	Source       string
	AsOf         string
	PTMean       *float64
	PTHigh       *float64
	PTLow        *float64
	PTMedian     *float64
	AnalystCount *int
	Buy          int
	Hold         int
	Sell         int
	Currency     string
	FetchedAt    time.Time
}
