package codegen

import (
	"fmt"
	"os"
	"path/filepath"

	"megane/internal/datamodeling"
	"megane/internal/db"
)

type ApplyOptions struct {
	SrcRoot   string // repo root (for prompt loading)
	ExportDir string // write to generated/<slug>/ if non-empty; else write in-place
	Force     bool   // overwrite files already in built_artifacts_json
}

// Apply runs all four generators and writes files.
// Returns the artifact list and any warnings (never fatal — warnings allow partial success).
func Apply(
	database *db.DB,
	typeID int64,
	opts ApplyOptions,
) (datamodeling.ApplyResponse, error) {
	resp := datamodeling.ApplyResponse{}

	detail, err := datamodeling.GetBuildDetail(database, typeID)
	if err != nil {
		return resp, fmt.Errorf("failed to get build detail: %w", err)
	}
	if detail.Schema == nil {
		return resp, fmt.Errorf("no schema exists for type")
	}

	resp.FileType = detail.FileType.Slug
	resp.SchemaVersion = detail.Schema.Version

	hasIncluded := false
	for _, r := range detail.FieldRows {
		if r.Override.Included {
			hasIncluded = true
			if r.Override.Emphasis == "critical" && r.SchemaField.Description == "" && r.Override.Rules == "" {
				resp.Warnings = append(resp.Warnings, fmt.Sprintf("Critical field %q has no description or rules", r.SchemaField.Name))
			}
		}
	}
	if !hasIncluded {
		return resp, fmt.Errorf("no fields included for build")
	}

	if !opts.Force && len(detail.Config.BuiltArtifacts.Files) > 0 {
		return resp, fmt.Errorf("artifacts already built; use ?force=true to overwrite")
	}

	var artifacts []datamodeling.Artifact

	baseDir := opts.SrcRoot
	if opts.ExportDir != "" {
		baseDir = filepath.Join(opts.ExportDir, detail.FileType.Slug)
	}

	// 1. Go Struct
	structSrc, err := GoStruct(detail.FileType.Slug, detail.FieldRows)
	if err != nil {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("struct format error: %v", err))
	}
	structPath := filepath.Join(baseDir, "internal", "models", detail.FileType.Slug+"_analysis.go")
	if opts.ExportDir != "" {
		structPath = filepath.Join(baseDir, "models", detail.FileType.Slug+"_analysis.go")
	}
	if err := writeFile(structPath, structSrc); err != nil {
		return resp, fmt.Errorf("write struct: %w", err)
	}
	artifacts = append(artifacts, datamodeling.Artifact{Path: structPath, Action: "created"})

	// 2. Production Prompt
	// We use the production prompt from config if it exists, otherwise generate it.
	promptText := detail.Config.ProductionPrompt
	if promptText == "" {
		// Assuming English default if not specified in JumpStartConfig (which we don't have here easily, but we can default to en)
		promptText = ProductionPrompt(detail.FileType.Name, detail.FieldRows, detail.Config, "en")
	}
	promptPath := filepath.Join(baseDir, "prompts", detail.FileType.Slug+".txt")
	if err := writeFile(promptPath, []byte(promptText)); err != nil {
		return resp, fmt.Errorf("write prompt: %w", err)
	}
	artifacts = append(artifacts, datamodeling.Artifact{Path: promptPath, Action: "created"})

	// 3. Route Snippet
	routeSrc := RouteSnippet(detail.FileType.Slug, detail.Config.MIMETypes, detail.StrategyConsensus)
	routePath := filepath.Join(baseDir, "route_snippet.go")
	if err := writeFile(routePath, []byte(routeSrc)); err != nil {
		return resp, fmt.Errorf("write route snippet: %w", err)
	}
	artifacts = append(artifacts, datamodeling.Artifact{Path: routePath, Action: "created", Note: "merge into cmd/server/main.go"})

	// 4. JS Renderer
	jsSrc := JSRenderer(detail.FileType.Slug, detail.FieldRows)
	jsPath := filepath.Join(baseDir, "renderer.js")
	if err := writeFile(jsPath, []byte(jsSrc)); err != nil {
		return resp, fmt.Errorf("write js renderer: %w", err)
	}
	artifacts = append(artifacts, datamodeling.Artifact{Path: jsPath, Action: "created", Note: "merge into static/js/analyze.js"})

	// Mark built
	buildArtifacts := datamodeling.BuildArtifacts{
		SchemaVersion: detail.Schema.Version,
		Files:         artifacts,
	}
	if err := datamodeling.MarkBuilt(database, typeID, buildArtifacts); err != nil {
		return resp, fmt.Errorf("mark built: %w", err)
	}

	resp.Artifacts = artifacts
	return resp, nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
