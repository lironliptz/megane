package datamodeling

import "testing"

func TestEstimateRequestTokens_MultiFileVision(t *testing.T) {
	files := []FileCostInput{
		{MimeType: "application/pdf", FileSize: 500_000, TextChars: 10, HasEmbeddedText: false},
		{MimeType: "application/pdf", FileSize: 600_000, TextChars: 12, HasEmbeddedText: false},
	}
	got := EstimateRequestTokens(files)
	// Each scanned PDF: 1000 + 5 pages * 1000 = 6000
	want := 12_000
	if got != want {
		t.Fatalf("EstimateRequestTokens() = %d, want %d", got, want)
	}
	if err := CheckCostTier(got, "premium"); err != nil {
		t.Fatalf("premium tier should allow multi-file vision: %v", err)
	}
}

func TestEstimateRequestTokens_TextUsesContentNotFileSize(t *testing.T) {
	files := []FileCostInput{
		{MimeType: "application/pdf", FileSize: 4 << 20, TextChars: 8_000, HasEmbeddedText: true},
	}
	got := EstimateRequestTokens(files)
	// 1000 + 8000/4 = 3000 — not inflated by 4MB file size on disk
	want := 3_000
	if got != want {
		t.Fatalf("EstimateRequestTokens() = %d, want %d", got, want)
	}
}

func TestCheckCostTier_StandardBlocksLargeEstimate(t *testing.T) {
	err := CheckCostTier(20_000, "standard")
	if err == nil {
		t.Fatal("expected cost threshold error for standard tier")
	}
}
