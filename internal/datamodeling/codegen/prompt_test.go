package codegen

import (
	"strings"
	"testing"

	"megane/internal/datamodeling"
)

func TestProductionPrompt_CriticalFieldsFirst(t *testing.T) {
	rows := []datamodeling.FieldRow{
		{
			SchemaField: datamodeling.SchemaField{Name: "normal_field", Type: "string"},
			Override:    datamodeling.FieldOverride{Included: true, Required: false, Emphasis: "normal"},
		},
		{
			SchemaField: datamodeling.SchemaField{Name: "critical_field", Type: "string"},
			Override:    datamodeling.FieldOverride{Included: true, Required: true, Emphasis: "critical"},
		},
	}
	cfg := datamodeling.BuildConfig{}

	prompt := ProductionPrompt("test", rows, cfg, "en")

	critIdx := strings.Index(prompt, "critical_field")
	normIdx := strings.Index(prompt, "normal_field")

	if critIdx == -1 || normIdx == -1 {
		t.Fatalf("Missing fields in prompt")
	}
	if critIdx > normIdx {
		t.Errorf("Critical field should appear before normal field")
	}
}

func TestProductionPrompt_ExcludedFieldsListed(t *testing.T) {
	rows := []datamodeling.FieldRow{
		{
			SchemaField: datamodeling.SchemaField{Name: "excluded_field", Type: "string"},
			Override:    datamodeling.FieldOverride{Included: false},
		},
	}
	cfg := datamodeling.BuildConfig{}

	prompt := ProductionPrompt("test", rows, cfg, "en")

	if !strings.Contains(prompt, "## Do NOT extract") {
		t.Errorf("Missing Do NOT extract section")
	}
	if !strings.Contains(prompt, "- excluded_field") {
		t.Errorf("Missing excluded field in Do NOT extract section")
	}
}

func TestProductionPrompt_HebrewPolicy(t *testing.T) {
	rows := []datamodeling.FieldRow{}
	cfg := datamodeling.BuildConfig{}

	prompt := ProductionPrompt("test", rows, cfg, "he")

	if !strings.Contains(prompt, "Hebrew values are acceptable") {
		t.Errorf("Missing Hebrew language policy")
	}
}
