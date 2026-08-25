package datamodeling

import "strings"

var CostTierTokenLimits = map[string]int{
	"budget":   4_000,
	"standard": 16_000,
	"premium":  128_000,
}

func DetectStrategy(mimeType string, fileSize int64, hasEmbeddedText bool) ProcessingStrategy {
	s := ProcessingStrategy{}

	switch {
	case strings.HasPrefix(mimeType, "image/"):
		s.ConvertToImage = true
		s.UseOCR = false
	case mimeType == "application/pdf":
		if hasEmbeddedText {
			s.ConvertToImage = false
			s.UseOCR = false
		} else {
			s.ConvertToImage = true
			s.UseOCR = true
		}
	case isExcelMIME(mimeType), isWordMIME(mimeType):
		s.ConvertToImage = false
		s.UseOCR = false
	default:
		s.ConvertToImage = false
		s.UseOCR = false
	}

	switch {
	case s.ConvertToImage && fileSize > 2<<20:
		s.EstimatedCostTier = "premium"
	case s.ConvertToImage:
		s.EstimatedCostTier = "standard"
	default:
		s.EstimatedCostTier = "budget"
	}

	return s
}

// FileCostInput carries per-file data for pre-flight token estimation.
type FileCostInput struct {
	MimeType        string
	FileSize        int64
	TextChars       int
	HasEmbeddedText bool
}

func EstimateTokens(strategy ProcessingStrategy, contentChars int, pageCount int) int {
	base := 1_000
	if strategy.ConvertToImage {
		if pageCount > 0 {
			base += pageCount * 1_000
		} else {
			base += 1_000
		}
	} else {
		base += contentChars / 4
	}
	if strategy.MultiPass && len(strategy.Passes) > 1 {
		base *= len(strategy.Passes)
	}
	return base
}

func visionPageEstimate(mimeType string, hasEmbeddedText bool) int {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return 1
	case mimeType == "application/pdf" && !hasEmbeddedText:
		return 5 // matches VisionPartsForFile maxPDFPages default
	default:
		return 0
	}
}

// EstimateRequestTokens sums per-file estimates for multi-sample analyze requests.
func EstimateRequestTokens(files []FileCostInput) int {
	total := 0
	for _, f := range files {
		strat := DetectStrategy(f.MimeType, f.FileSize, f.HasEmbeddedText)
		chars := f.TextChars
		if chars == 0 && !strat.ConvertToImage {
			chars = int(f.FileSize)
		}
		total += EstimateTokens(strat, chars, visionPageEstimate(f.MimeType, f.HasEmbeddedText))
	}
	return total
}

func CheckCostTier(estimated int, maxCostTier string) error {
	if maxCostTier == "" {
		maxCostTier = "standard"
	}
	limit, ok := CostTierTokenLimits[maxCostTier]
	if !ok {
		limit = CostTierTokenLimits["standard"]
	}
	if estimated > limit {
		return ErrCostThresholdExceeded{Estimated: estimated, Limit: limit, Tier: maxCostTier}
	}
	return nil
}

func isExcelMIME(m string) bool {
	return m == "application/vnd.ms-excel" ||
		m == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

func isWordMIME(m string) bool {
	return m == "application/msword" ||
		m == "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
}
