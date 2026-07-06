// Package handler provides HTTP handlers for the proxy endpoints.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/major1201/anthropic-proxy-go/internal/client"
	"github.com/major1201/anthropic-proxy-go/internal/common"
	"github.com/major1201/anthropic-proxy-go/internal/config"
	"github.com/major1201/anthropic-proxy-go/internal/converter"
	"github.com/major1201/anthropic-proxy-go/internal/models"
)

// MessagesHandler handles /v1/messages requests.
type MessagesHandler struct {
	config         *config.Config
	requestConv    *converter.RequestConverter
	responseConv   *converter.ResponseConverter
	openaiClient   *client.OpenAIClient
}

// NewMessagesHandler creates a new MessagesHandler.
func NewMessagesHandler(cfg *config.Config) *MessagesHandler {
	return &MessagesHandler{
		config:       cfg,
		requestConv:  converter.NewRequestConverter(),
		responseConv: converter.NewResponseConverter(),
		openaiClient: client.NewOpenAIClient(120 * time.Second),
	}
}

// ServeHTTP handles the /v1/messages POST endpoint.
func (h *MessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := common.GetRequestID(r.Context())
	logger := common.LoggerWithRequestID(r.Context())

	clientIP := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		clientIP = forwarded
	}

	logger.Info("received Anthropic request",
		"method", r.Method,
		"url", r.URL.String(),
		"ip", clientIP,
	)

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body", requestID)
		return
	}

	var req models.AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeValidationError(w, fmt.Sprintf("invalid request format: %v", err), requestID)
		return
	}

	// Log request (without tools for brevity)
	logBody := make(map[string]interface{})
	json.Unmarshal(body, &logBody)
	delete(logBody, "tools")
	logBodyJSON, _ := json.MarshalIndent(logBody, "", "  ")
	logger.Debug("Anthropic request body", "body", string(logBodyJSON))

	// Validate required fields
	if err := validateRequest(&req); err != nil {
		writeValidationError(w, err.Error(), requestID)
		return
	}

	// Strip anthropic billing header from system
	req = purifyRequest(req, logger)

	stream := req.Stream != nil && *req.Stream

	if stream {
		h.handleStream(w, r, &req, requestID, logger)
	} else {
		h.handleNonStream(w, r, &req, requestID, logger)
	}
}

func (h *MessagesHandler) handleNonStream(w http.ResponseWriter, r *http.Request, req *models.AnthropicRequest, requestID string, logger *slog.Logger) {
	route, ok := h.config.GetRoute(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("model mapping not found for model: %s", req.Model), requestID)
		return
	}

	// Convert request
	openaiReq, err := h.requestConv.Convert(r.Context(), req, requestID)
	if err != nil {
		logger.Error("request conversion failed", "error", err)
		writeError(w, http.StatusBadRequest, err.Error(), requestID)
		return
	}

	// Send to OpenAI
	openaiResp, err := h.openaiClient.SendRequest(r.Context(), route.BaseURL, route.APIKey, openaiReq, requestID)
	if err != nil {
		logger.Error("OpenAI request failed", "error", err)
		if clientErr, ok := err.(*client.ClientError); ok {
			writeErrorResponse(w, getHTTPStatus(clientErr), clientErr.ErrorResponse, requestID)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), requestID)
		return
	}

	// Convert response
	anthropicResp, err := h.responseConv.ConvertResponse(openaiResp, req.Model, requestID)
	if err != nil {
		logger.Error("response conversion failed", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error(), requestID)
		return
	}

	// Log response text
	responseText := "empty"
	if len(anthropicResp.Content) > 0 && anthropicResp.Content[0].Text != nil {
		responseText = truncateText(*anthropicResp.Content[0].Text, 100)
	}
	logger.Info("Anthropic response generation complete",
		"text", responseText,
		"usage", anthropicResp.Usage,
	)

	// Write response
	respBody, _ := json.Marshal(anthropicResp)
	w.Header().Set("Content-Type", "application/json")
	if requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

func (h *MessagesHandler) handleStream(w http.ResponseWriter, r *http.Request, req *models.AnthropicRequest, requestID string, logger *slog.Logger) {
	route, ok := h.config.GetRoute(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("model mapping not found for model: %s", req.Model), requestID)
		return
	}

	// Convert request
	openaiReq, err := h.requestConv.Convert(r.Context(), req, requestID)
	if err != nil {
		logger.Error("request conversion failed", "error", err)
		writeStreamError(w, err.Error(), requestID)
		return
	}

	// Send streaming request
	streamCh, err := h.openaiClient.SendStreamingRequest(r.Context(), route.BaseURL, route.APIKey, openaiReq, requestID)
	if err != nil {
		logger.Error("OpenAI streaming request failed", "error", err)
		writeStreamError(w, err.Error(), requestID)
		return
	}

	// Set streaming headers
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "*")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("streaming not supported")
		return
	}

	// Create a pipe to connect the raw SSE channel to the converter
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		for line := range streamCh {
			fmt.Fprintf(pw, "%s\n", line)
		}
	}()

	// Convert and write events
	logger.Info("starting streaming conversion")
	eventCh := h.responseConv.ConvertStream(r.Context(), pr, req.Model, requestID)

	for evt := range eventCh {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		logger.Debug("Anthropic event", "event", evt.Event)
		fmt.Fprint(w, evt.FormatSSE())
		flusher.Flush()
	}

	logger.Info("streaming conversion complete")
}

func validateRequest(req *models.AnthropicRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model field cannot be empty")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("message list cannot be empty")
	}
	if req.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be a positive integer")
	}
	if req.Temperature != nil && (*req.Temperature < 0.0 || *req.Temperature > 1.0) {
		return fmt.Errorf("temperature must be between 0.0 and 1.0")
	}
	if req.TopP != nil && (*req.TopP < 0.0 || *req.TopP > 1.0) {
		return fmt.Errorf("top_p must be between 0.0 and 1.0")
	}
	for _, msg := range req.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			return fmt.Errorf("message role must be 'user' or 'assistant', got: %s", msg.Role)
		}
	}
	return nil
}

func purifyRequest(req models.AnthropicRequest, logger *slog.Logger) models.AnthropicRequest {
	var sysMsgs []models.AnthropicSystemMessage
	if err := json.Unmarshal(req.System, &sysMsgs); err == nil && len(sysMsgs) > 0 {
		if strings.HasPrefix(sysMsgs[0].Text, "x-anthropic-billing-header:") {
			req.System, _ = json.Marshal(sysMsgs[1:])
		}
	}
	return req
}

// HealthHandler returns the health check handler.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"service":   "anthropic-proxy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"checks":    map[string]interface{}{},
	})
}

// RootHandler returns the root handler.
func RootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Welcome to the Anthropic-OpenAI Proxy (Go)",
	})
}

func writeError(w http.ResponseWriter, statusCode int, message string, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	errorResp := models.GetErrorResponse(statusCode, message, map[string]interface{}{
		"request_id": requestID,
	})
	json.NewEncoder(w).Encode(errorResp)
}

func writeErrorResponse(w http.ResponseWriter, statusCode int, errorResp *models.StandardErrorResponse, requestID string) {
	if errorResp.Error.Details == nil {
		errorResp.Error.Details = make(map[string]interface{})
	}
	errorResp.Error.Details["request_id"] = requestID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errorResp)
}

func writeValidationError(w http.ResponseWriter, message string, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	errorResp := models.GetErrorResponse(422, message, map[string]interface{}{
		"request_id": requestID,
	})
	json.NewEncoder(w).Encode(errorResp)
}

func writeStreamError(w http.ResponseWriter, message string, requestID string) {
	errorData := map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": message,
		},
	}
	if requestID != "" {
		errorData["request_id"] = requestID
	}
	data, _ := json.Marshal(errorData)
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(data))
}

func getHTTPStatus(err *client.ClientError) int {
	code := err.ErrorResponse.Error.Code
	switch code {
	case "bad_request":
		return http.StatusBadRequest
	case "unauthorized":
		return http.StatusUnauthorized
	case "not_found":
		return http.StatusNotFound
	case "validation_error":
		return http.StatusUnprocessableEntity
	case "rate_limit_exceeded":
		return http.StatusTooManyRequests
	case "timeout":
		return http.StatusGatewayTimeout
	case "external_service_error":
		return http.StatusBadGateway
	case "service_unavailable":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
