package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadPrompts reads all .txt files in dir and returns a map of base-name (without
// extension) -> file content. E.g. "prompts/analyze.txt" becomes key "analyze".
func LoadPrompts(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load prompts: read dir %q: %w", dir, err)
	}
	prompts := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".txt") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load prompts: read %q: %w", path, err)
		}
		key := strings.TrimSuffix(name, ".txt")
		prompts[key] = string(data)
	}
	return prompts, nil
}
