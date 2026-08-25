package admin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJumpstartVSCodeSettings_matchesTemplateKeys(t *testing.T) {
	cfg := JumpStartConfig{
		AppName:      "My Sprout",
		PrimaryColor: "#ea580c",
	}
	root := jumpstartVSCodeSettingsRoot(cfg)

	if root["window.title"] != "My Sprout - Cursor" {
		t.Fatalf("window.title = %v", root["window.title"])
	}
	if _, ok := root["peacock.color"]; !ok {
		t.Fatal("missing peacock.color")
	}

	wb, ok := root["workbench.colorCustomizations"].(map[string]string)
	if !ok {
		t.Fatal("workbench.colorCustomizations not map[string]string")
	}
	for _, key := range jumpstartTemplateWorkbenchColorKeys {
		if _, ok := wb[key]; !ok {
			t.Errorf("missing workbench color key %q", key)
		}
	}
	if len(wb) != len(jumpstartTemplateWorkbenchColorKeys) {
		t.Errorf("workbench key count %d, want %d (extra keys: check jumpstart_vscode.go vs template)",
			len(wb), len(jumpstartTemplateWorkbenchColorKeys))
	}
}

func TestJumpstartVSCodeSettingsJSON_valid(t *testing.T) {
	cfg := JumpStartConfig{AppName: "Test App", PrimaryColor: "#4f8ef7"}
	raw, err := jumpstartVSCodeSettingsJSON(cfg)
	if err != nil {
		t.Fatal(err)
	}
	combined := vscodeSettingsFilePreamble + string(raw)
	// Strip JSONC comments for json.Unmarshal
	lines := strings.Split(combined, "\n")
	var clean []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//") {
			continue
		}
		clean = append(clean, line)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.Join(clean, "\n")), &parsed); err != nil {
		t.Fatalf("generated settings not valid JSON: %v\n%s", err, combined)
	}
}
