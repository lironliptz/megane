package version

import "strings"

// LinkVersion is set at link time via:
//
//	-ldflags "-X jump-starter/internal/version.LinkVersion=<semver>"
//
// Makefile and Dockerfile read the repo-root VERSION file and inject this.
// Empty means plain `go run ./cmd/server` without ldflags (reports as dev).
var LinkVersion string

// Name is the template / distribution id for operators and sprout apps.
const Name = "jump-starter"

// Version returns the jump-starter template semantic version.
func Version() string {
	v := strings.TrimSpace(LinkVersion)
	if v != "" {
		return v
	}
	return "dev"
}
