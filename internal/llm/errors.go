package llm

import "errors"

// ErrNoClient is returned when CompleteGated is called without a client.
var ErrNoClient = errors.New("llm: client not configured")
