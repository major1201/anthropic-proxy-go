package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/major1201/anthropic-proxy-go/internal/config"
	"github.com/major1201/anthropic-proxy-go/internal/handler"
)

func setupErrorTestServer(t *testing.T) (*httptest.Server, *handler.MessagesHandler) {
	t.Helper()

	mockOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")

		// Check messages for error trigger keywords
		messages, _ := req["messages"].([]interface{})
		var trigger string
		if len(messages) > 0 {
			if msg, ok := messages[0].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					trigger = content
				}
			}
		}

		switch trigger {
		case "trigger-429":
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Rate limit exceeded",
					"type":    "rate_limit_exceeded",
				},
			})
		case "trigger-500":
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Internal server error",
					"type":    "server_error",
				},
			})
		case "trigger-503":
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Service unavailable",
					"type":    "server_error",
				},
			})
		default:
			respModel, _ := req["model"].(string)
			resp := map[string]interface{}{
				"id":      "chatcmpl-error-test",
				"object":  "chat.completion",
				"created": 1700000000,
				"model":   respModel,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "OK",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":     5,
					"completion_tokens": 5,
					"total_tokens":      10,
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	t.Cleanup(mockOpenAI.Close)

	baseURL := mockOpenAI.URL
	cfg := config.DefaultConfig()
	cfg.Routes = map[string]config.OpenAIModelConfig{
		"error-429": {
			BaseURL: baseURL,
			APIKey:  "test-openai-key",
		},
		"error-500": {
			BaseURL: baseURL,
			APIKey:  "test-openai-key",
		},
		"error-503": {
			BaseURL: baseURL,
			APIKey:  "test-openai-key",
		},
		"__default__": {
			BaseURL: baseURL,
			APIKey:  "test-openai-key",
		},
	}
	config.SetConfig(cfg)

	return mockOpenAI, handler.NewMessagesHandler(cfg)
}

func TestErrorHandling_RateLimit(t *testing.T) {
	_, msgHandler := setupErrorTestServer(t)

	body := `{
		"model": "error-429",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "trigger-429"}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", w.Code, w.Body.String())
	}

	var errorResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &errorResp)
	if errorResp["type"] != "error" {
		t.Error("expected error type in response")
	}
}

func TestErrorHandling_ServerError(t *testing.T) {
	_, msgHandler := setupErrorTestServer(t)

	body := `{
		"model": "error-500",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "trigger-500"}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}

func TestErrorHandling_ServiceUnavailable(t *testing.T) {
	_, msgHandler := setupErrorTestServer(t)

	body := `{
		"model": "error-503",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "trigger-503"}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", w.Code)
	}
}

func TestErrorHandling_InvalidJSON(t *testing.T) {
	_, msgHandler := setupErrorTestServer(t)

	body := `not json at all`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestErrorHandling_ErrorResponseFormat(t *testing.T) {
	_, msgHandler := setupErrorTestServer(t)

	// Use a model that doesn't exist in routes and doesn't fall through to __default__
	// Remove __default__ to test model-not-found behavior
	cfg := config.GetConfig()
	delete(cfg.Routes, "__default__")

	body := `{
		"model": "unknown-route",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "test"}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	var errorResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &errorResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if errorResp["type"] != "error" {
		t.Error("expected type 'error' in response")
	}

	errorDetail, ok := errorResp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error detail in response")
	}

	if errorDetail["code"] == nil {
		t.Error("expected code in error detail")
	}
	if errorDetail["message"] == nil {
		t.Error("expected message in error detail")
	}
}
