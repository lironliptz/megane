package filedb

import (
	"strings"
)

// cikMaxDigits is the SEC's fixed-width CIK length.
const cikMaxDigits = 10

// NormalizeCIK canonicalizes user- or URL-supplied CIKs to the zero-padded
// 10-digit form used as the directory name under fileDB/companies/.
//
// It accepts "1567529", "0001567529", and "CIK0001567529". Anything that is not
// 1..10 decimal digits after trimming is rejected with ErrInvalidCIK, which is
// what keeps path traversal out of filepath.Join by construction: no input that
// fails this check ever reaches the filesystem.
func NormalizeCIK(s string) (string, error) {
	t := strings.TrimSpace(s)
	if hasCIKPrefix(t) {
		t = t[3:]
	}
	t = strings.TrimSpace(t)
	if t == "" || len(t) > cikMaxDigits {
		return "", ErrInvalidCIK
	}
	for i := 0; i < len(t); i++ {
		if t[i] < '0' || t[i] > '9' {
			return "", ErrInvalidCIK
		}
	}
	return strings.Repeat("0", cikMaxDigits-len(t)) + t, nil
}

func hasCIKPrefix(s string) bool {
	return len(s) > 3 && strings.EqualFold(s[:3], "cik")
}

// trimCIKZeros strips leading zeros for display and prefix matching, so a user
// typing "1567529" matches the stored "0001567529".
func trimCIKZeros(s string) string {
	t := strings.TrimLeft(s, "0")
	if t == "" {
		return "0"
	}
	return t
}
