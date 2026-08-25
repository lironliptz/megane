package pipeline

import (
	"os"
	"strconv"
	"strings"
)

const defaultMaxVisionImageBytes = 15 << 20 // 15 MiB

// maxVisionImageBytes caps raw image payload size for Gemini vision (env LLM_MAX_IMAGE_BYTES).
func maxVisionImageBytes() int {
	s := strings.TrimSpace(os.Getenv("LLM_MAX_IMAGE_BYTES"))
	if s == "" {
		return defaultMaxVisionImageBytes
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultMaxVisionImageBytes
	}
	return v
}
