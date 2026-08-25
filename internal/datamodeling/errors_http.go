package datamodeling

import (
	"context"
	"errors"
	"net/http"

	"megane/internal/llm"
)

// AnalysisErrorBody is stored on failed jobs and returned to the admin UI.
type AnalysisErrorBody struct {
	Code    string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// ClassifyAnalysisError maps analyzer errors to HTTP status + JSON body.
func ClassifyAnalysisError(err error) (status int, body AnalysisErrorBody) {
	if err == nil {
		return http.StatusInternalServerError, AnalysisErrorBody{Code: "analysis_failed", Message: "unknown error"}
	}
	var e1 ErrInvalidFileIDs
	var e2 ErrInvalidLLMResponse
	var e3 ErrCostThresholdExceeded
	switch {
	case errors.As(err, &e1):
		return http.StatusBadRequest, AnalysisErrorBody{Code: "invalid_file_ids", Message: err.Error()}
	case errors.As(err, &e2):
		return http.StatusUnprocessableEntity, AnalysisErrorBody{
			Code: "invalid_llm_response", Message: err.Error(), Details: e2.Details,
		}
	case errors.As(err, &e3):
		return http.StatusTooManyRequests, AnalysisErrorBody{Code: "cost_threshold_exceeded", Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, AnalysisErrorBody{Code: "timeout", Message: "analysis timed out"}
	default:
		return http.StatusInternalServerError, AnalysisErrorBody{
			Code: "analysis_failed", Message: llm.SafeErr(err),
		}
	}
}
