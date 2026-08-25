package datamodeling

import "strings"

// normaliseFieldName strips separators for fuzzy field-name comparison.
func normaliseFieldName(s string) string {
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(s)))
}

// fieldNamesSimilar reports whether two schema field names likely refer to the same concept.
func fieldNamesSimilar(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	na, nb := normaliseFieldName(a), normaliseFieldName(b)
	if na == nb {
		return true
	}
	al, bl := strings.ToLower(a), strings.ToLower(b)
	if strings.Contains(al, bl) || strings.Contains(bl, al) {
		return true
	}
	// Shared snake_case stem, e.g. invoice_number ↔ invoice_no
	if sharedFieldStem(na, nb) {
		return true
	}
	return false
}

func sharedFieldStem(a, b string) bool {
	if len(a) < 4 || len(b) < 4 {
		return false
	}
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	threshold := minLen - 2
	if threshold < 6 {
		threshold = 6
	}
	if threshold > minLen {
		threshold = minLen
	}
	prefix := 0
	for prefix < minLen && a[prefix] == b[prefix] {
		prefix++
	}
	return prefix >= threshold
}

// matchDiscoveredField finds a discovered field name that corresponds to canonicalName.
// discovered is the set of field names extracted in the current document run.
func matchDiscoveredField(canonicalName string, discovered map[string]string, used map[string]bool) (matched string, ok bool) {
	if len(discovered) == 0 {
		return "", false
	}

	// 1. Exact name (case-insensitive)
	for name := range discovered {
		if used[name] {
			continue
		}
		if strings.EqualFold(name, canonicalName) {
			return name, true
		}
	}

	// 2. Normalised match
	canonNorm := normaliseFieldName(canonicalName)
	for name := range discovered {
		if used[name] {
			continue
		}
		if normaliseFieldName(name) == canonNorm {
			return name, true
		}
	}

	// 3. Substring / partial overlap (longer name must be at least 4 chars to avoid noise)
	for name := range discovered {
		if used[name] {
			continue
		}
		if fieldNamesSimilar(canonicalName, name) {
			return name, true
		}
	}

	return "", false
}
