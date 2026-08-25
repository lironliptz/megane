package filedb

import (
	"os"
	"path/filepath"
	"testing"
)

// fixtureRoot copies internal/filedb/testdata into a temp dir and injects the
// .DS_Store files the real corpus contains inside its year folders.
//
// They are injected rather than committed because .gitignore:54 ignores
// .DS_Store — a committed fixture copy would vanish on a fresh clone and the
// "skip non-directory entries" test would quietly stop testing anything.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := "testdata"

	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copying fixture: %v", err)
	}

	for _, year := range []string{"2016", "2026"} {
		p := filepath.Join(dst, "companies", "0000000001", year, ".DS_Store")
		if err := os.WriteFile(p, []byte("\x00\x00\x00\x01Bud1"), 0o644); err != nil {
			t.Fatalf("injecting .DS_Store: %v", err)
		}
	}
	return dst
}
