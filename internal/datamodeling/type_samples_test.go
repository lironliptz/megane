package datamodeling

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("hello datamodeling"), 0644); err != nil {
		t.Fatal(err)
	}
	h1, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h1) != 64 {
		t.Fatalf("hash len = %d, want 64 hex chars", len(h1))
	}
	h2, err := HashFile(path)
	if err != nil || h1 != h2 {
		t.Fatalf("hash not stable: %q vs %q", h1, h2)
	}
}
