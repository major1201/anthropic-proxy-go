// Package models provides standardized error response types.
package models

// ErrorDetail is standardized error detail.
type ErrorDetail struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Param     *string                `json:"param,omitempty"`
	Type      *string                `json:"type,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	RequestID *string                `json:"request_id,omitempty"`
}

// StandardErrorResponse is the standard error envelope.
type StandardErrorResponse struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}

// ValidationErrorItem is a single validation error.
type ValidationErrorItem struct {
	Loc  []string `json:"loc"`
	Msg  string   `json:"msg"`
	Type string   `json:"type"`
}

// errorSpec defines the default code, message, and type for an HTTP status.
type errorSpec struct {
	code    string
	message string
	typ     string
}

var errorMapping = map[int]errorSpec{
	400: {"bad_request", "Invalid request format or parameters", "invalid_request_error"},
	401: {"unauthorized", "Invalid API key or unauthorized access", "authentication_error"},
	404: {"not_found", "The requested resource does not exist", "not_found_error"},
	422: {"validation_error", "Request parameter validation failed", "invalid_request_error"},
	429: {"rate_limit_exceeded", "Request rate limit exceeded, please try again later", "rate_limit_error"},
	500: {"internal_server_error", "Internal server error, please try again later", "server_error"},
	502: {"external_service_error", "External service error, please try again later", "api_error"},
	503: {"service_unavailable", "Service temporarily unavailable, please try again later", "server_error"},
	504: {"timeout", "Request timeout, please try again later", "timeout_error"},
}

// GetErrorResponse returns a StandardErrorResponse for the given HTTP status code.
func GetErrorResponse(statusCode int, message string, details map[string]interface{}) *StandardErrorResponse {
	spec, ok := errorMapping[statusCode]
	if !ok {
		spec = errorMapping[500]
	}

	errMsg := spec.message
	if message != "" {
		errMsg = message
	}

	errDetail := ErrorDetail{
		Code:    spec.code,
		Message: errMsg,
		Details: details,
	}

	typ := spec.typ
	errDetail.Type = &typ

	// Special handling for validation errors
	if statusCode == 422 && details != nil {
		if validationErrors, ok := details["validation_errors"]; ok {
			errDetail.Details = map[string]interface{}{
				"validation_errors": validationErrors,
			}
		}
	}

	// Add optional fields from details
	if details != nil {
		if v, ok := details["param"]; ok {
			if s, ok := v.(string); ok {
				errDetail.Param = &s
			}
		}
		if v, ok := details["request_id"]; ok {
			if s, ok := v.(string); ok {
				errDetail.RequestID = &s
			}
		}
		if v, ok := details["retry_after"]; ok {
			errDetail.Details["retry_after"] = v
		}
		if v, ok := details["service"]; ok {
			errDetail.Details["service"] = v
		}
		if v, ok := details["original_error"]; ok {
			errDetail.Details["original_error"] = v
		}
	}

	return &StandardErrorResponse{
		Type:  "error",
		Error: errDetail,
	}
}
