package main

import (
	"os"
	"testing"
)

// writeFile is a small helper shared by this package's tests: write content
// to path, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
