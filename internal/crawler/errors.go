package crawler

import "errors"

var (
	// ErrElementNotFound is returned when a selector matches no elements within the timeout.
	ErrElementNotFound = errors.New("crawler: element not found")

	// ErrNoLinksMatched is returned when link collection yields an empty set.
	ErrNoLinksMatched = errors.New("crawler: no links matched selectors")
)
