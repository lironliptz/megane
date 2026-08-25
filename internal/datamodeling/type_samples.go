package datamodeling

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"time"

	"megane/internal/db"
)

// TypeSample is a file already used to build or refine a file type's schema.
type TypeSample struct {
	ID            int64
	FileTypeID    int64
	FileID        int64
	OriginalName  string
	SizeBytes     int64
	ContentHash   string
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	AnalysisCount int
}

// TypeSampleView is the API shape for a registered sample file.
type TypeSampleView struct {
	ID            int64  `json:"id"`
	FileID        int64  `json:"file_id"`
	OriginalName  string `json:"original_name"`
	SizeBytes     int64  `json:"size_bytes"`
	ContentHash   string `json:"content_hash,omitempty"`
	FirstSeenAt   string `json:"first_seen_at"`
	LastSeenAt    string `json:"last_seen_at"`
	AnalysisCount int    `json:"analysis_count"`
}

// DuplicateMatch describes how an upload matches a prior sample.
type DuplicateMatch struct {
	MatchType string         `json:"match_type"` // exact | name_size
	Sample    TypeSampleView `json:"sample"`
}

// SampleRegisterInput is one file to record after a successful analysis.
type SampleRegisterInput struct {
	FileID       int64
	OriginalName string
	SizeBytes    int64
	ContentHash  string
	FilePath     string // on-disk path; hashed when ContentHash is empty
}

// HashFile returns the SHA-256 hex digest of a file on disk.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func typeSampleViewFrom(s TypeSample) TypeSampleView {
	return TypeSampleView{
		ID:            s.ID,
		FileID:        s.FileID,
		OriginalName:  s.OriginalName,
		SizeBytes:     s.SizeBytes,
		ContentHash:   s.ContentHash,
		FirstSeenAt:   s.FirstSeenAt.Format(time.RFC3339),
		LastSeenAt:    s.LastSeenAt.Format(time.RFC3339),
		AnalysisCount: s.AnalysisCount,
	}
}

func scanTypeSample(row interface {
	Scan(dest ...any) error
}) (*TypeSample, error) {
	s := &TypeSample{}
	if err := row.Scan(
		&s.ID, &s.FileTypeID, &s.FileID, &s.OriginalName, &s.SizeBytes, &s.ContentHash,
		&s.FirstSeenAt, &s.LastSeenAt, &s.AnalysisCount,
	); err != nil {
		return nil, err
	}
	return s, nil
}

// ListTypeSamples returns files previously analyzed for a file type (newest first).
func ListTypeSamples(database *db.DB, fileTypeID int64) ([]TypeSampleView, error) {
	rows, err := database.Query(
		`SELECT id, file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count
		 FROM dm_type_samples WHERE file_type_id = ? ORDER BY last_seen_at DESC, id DESC`, fileTypeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TypeSampleView
	for rows.Next() {
		s, err := scanTypeSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, typeSampleViewFrom(*s))
	}
	return out, rows.Err()
}

// FindDuplicateSample reports whether name/size/hash matches an existing sample for the type.
func FindDuplicateSample(database *db.DB, fileTypeID int64, originalName string, sizeBytes int64, contentHash string) (*DuplicateMatch, error) {
	contentHash = strings.TrimSpace(contentHash)
	if contentHash != "" {
		row := database.QueryRow(
			`SELECT id, file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count
			 FROM dm_type_samples WHERE file_type_id = ? AND content_hash = ? LIMIT 1`, fileTypeID, contentHash,
		)
		s, err := scanTypeSample(row)
		if err == nil {
			v := typeSampleViewFrom(*s)
			return &DuplicateMatch{MatchType: "exact", Sample: v}, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}

	rows, err := database.Query(
		`SELECT id, file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count
		 FROM dm_type_samples WHERE file_type_id = ? AND size_bytes = ?`, fileTypeID, sizeBytes,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	target := strings.TrimSpace(originalName)
	for rows.Next() {
		s, err := scanTypeSample(rows)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(s.OriginalName, target) {
			v := typeSampleViewFrom(*s)
			return &DuplicateMatch{MatchType: "name_size", Sample: v}, nil
		}
	}
	return nil, rows.Err()
}

// CheckSamplesByNameSize finds prior samples matching client-provided name+size (pre-upload).
func CheckSamplesByNameSize(database *db.DB, fileTypeID int64, names []string, sizes []int64) ([]DuplicateMatch, error) {
	if len(names) == 0 || len(names) != len(sizes) {
		return nil, nil
	}
	var matches []DuplicateMatch
	for i := range names {
		m, err := FindDuplicateSample(database, fileTypeID, names[i], sizes[i], "")
		if err != nil {
			return nil, err
		}
		if m != nil {
			matches = append(matches, *m)
		}
	}
	return matches, nil
}

// RegisterTypeSamples records analyzed files for a type (upsert by hash, else name+size).
func RegisterTypeSamples(database *db.DB, fileTypeID int64, inputs []SampleRegisterInput) error {
	now := time.Now().UTC()
	for _, in := range inputs {
		hash := strings.TrimSpace(in.ContentHash)
		if hash == "" && strings.TrimSpace(in.FilePath) != "" {
			if h, err := HashFile(in.FilePath); err == nil {
				hash = h
			}
		}

		var existing *TypeSample
		if hash != "" {
			row := database.QueryRow(
				`SELECT id, file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count
				 FROM dm_type_samples WHERE file_type_id = ? AND content_hash = ?`, fileTypeID, hash,
			)
			s, err := scanTypeSample(row)
			if err == nil {
				existing = s
			} else if err != sql.ErrNoRows {
				return err
			}
		}
		if existing == nil {
			rows, err := database.Query(
				`SELECT id, file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count
				 FROM dm_type_samples WHERE file_type_id = ? AND size_bytes = ?`, fileTypeID, in.SizeBytes,
			)
			if err != nil {
				return err
			}
			target := strings.TrimSpace(in.OriginalName)
			for rows.Next() {
				s, err := scanTypeSample(rows)
				if err != nil {
					rows.Close()
					return err
				}
				if strings.EqualFold(s.OriginalName, target) {
					existing = s
					break
				}
			}
			rows.Close()
		}

		if existing != nil {
			newHash := existing.ContentHash
			if newHash == "" && hash != "" {
				newHash = hash
			}
			_, err := database.Exec(
				`UPDATE dm_type_samples SET file_id = ?, last_seen_at = ?, analysis_count = analysis_count + 1,
				 content_hash = CASE WHEN content_hash = '' AND ? != '' THEN ? ELSE content_hash END
				 WHERE id = ?`,
				in.FileID, now, hash, hash, existing.ID,
			)
			if err != nil {
				return err
			}
			continue
		}

		_, err := database.Exec(
			`INSERT INTO dm_type_samples (file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
			fileTypeID, in.FileID, strings.TrimSpace(in.OriginalName), in.SizeBytes, hash, now, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// AnnotateFileProgressDuplicates marks progress items when uploads match prior samples.
func AnnotateFileProgressDuplicates(database *db.DB, fileTypeID int64, items []FileProgressItem, fileMeta map[int64]struct {
	Hash string
	Size int64
}) []FileProgressItem {
	for i := range items {
		meta, ok := fileMeta[items[i].FileID]
		if !ok {
			continue
		}
		match, err := FindDuplicateSample(database, fileTypeID, items[i].Name, meta.Size, meta.Hash)
		if err != nil || match == nil {
			continue
		}
		items[i].IsDuplicate = true
		switch match.MatchType {
		case "exact":
			items[i].DuplicateNote = "Duplicate — same file content as a prior sample"
		default:
			items[i].DuplicateNote = "Likely duplicate — same name and size as a prior sample"
		}
	}
	return items
}
