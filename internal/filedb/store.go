package filedb

import (
	"context"
	"errors"
)

// Sentinel errors. Handlers map these to HTTP status codes and must never
// inspect filesystem errors (os.ErrNotExist, *fs.PathError) directly — doing so
// would break the 404 path when the SQLite-backed store replaces FileDBStore.
var (
	// ErrCompanyNotFound means no company exists for the requested CIK.
	ErrCompanyNotFound = errors.New("filedb: company not found")
	// ErrInvalidCIK means the supplied CIK was not 1..10 decimal digits.
	ErrInvalidCIK = errors.New("filedb: invalid cik")
)

// CompanyStore is the read model over companies and their filings.
//
// Phase 1 implementation: FileDBStore, reading fileDB/companies/.
// Phase 2 implementation: a SQLite-backed store with identical signatures.
type CompanyStore interface {
	ListCompanies(ctx context.Context) ([]CompanySummary, error)
	SearchCompanies(ctx context.Context, query string, limit int) ([]CompanySummary, error)
	GetCompany(ctx context.Context, cik string) (*CompanyDetail, error)
	ListFilings(ctx context.Context, cik string, f FilingFilter) (FilingPage, error)
}
