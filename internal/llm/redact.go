package llm

import (
	"fmt"
	"regexp"
)

var (
	querySecretParam = regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|access_token|token|secret|password|passwd)=)[^&\s"']+`)
	bearerToken      = regexp.MustCompile(`(?i)\bBearer\s+[^\s"']+`)
	basicAuth        = regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]+`)
)

// RedactSecrets removes API keys, tokens, and passwords from a string so it is safe to log or return.
func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	out := querySecretParam.ReplaceAllString(s, `${1}[REDACTED]`)
	out = bearerToken.ReplaceAllString(out, "Bearer [REDACTED]")
	out = basicAuth.ReplaceAllString(out, "Basic [REDACTED]")
	return out
}

// SafeErr returns a redacted error message suitable for logs and API responses.
func SafeErr(err error) string {
	if err == nil {
		return ""
	}
	return RedactSecrets(err.Error())
}

// sanitizedError wraps an error message that has already been redacted.
type sanitizedError struct {
	msg string
}

func (e *sanitizedError) Error() string { return e.msg }

// WrapSanitizedErr returns an error whose Error() never contains secrets from the underlying err.
func WrapSanitizedErr(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return &sanitizedError{msg: fmt.Sprintf("%s: %s", prefix, RedactSecrets(err.Error()))}
}
