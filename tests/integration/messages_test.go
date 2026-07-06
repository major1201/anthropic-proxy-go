package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/major1201/anthropic-proxy-go/internal/config"
	"github.com/major1201/anthropic-proxy-go/internal/handler"
	"github.com/major1201/anthropic-proxy-go/internal/middleware"
)

func setupTestServer(t *testing.T) (*httptest.Server, *handler.MessagesHandler) {
	t.Helper()

	// Create a mock OpenAI backend
	mockOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")

		// Check auth
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-openai-key" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Invalid API key",
					"type":    "authentication_error",
				},
			})
			return
		}

		// Check for invalid model
		if model, ok := req["model"].(string); ok && model == "invalid-model" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Model not found",
					"type":    "invalid_request_error",
				},
			})
			return
		}

		resp := map[string]interface{}{
			"id":      "chatcmpl-test-123",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   req["model"],
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello! I'm a mock response. How can I help you today?",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 15,
				"total_tokens":      25,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(mockOpenAI.Close)

	// Create config pointing to mock backend
	baseURL := mockOpenAI.URL
	cfg := config.DefaultConfig()
	cfg.Routes = map[string]config.OpenAIModelConfig{
		"__default__": {
			BaseURL: baseURL,
			APIKey:  "test-openai-key",
			Model:   strPtr("gpt-4o"),
		},
	}
	config.SetConfig(cfg)

	msgHandler := handler.NewMessagesHandler(cfg)

	return mockOpenAI, msgHandler
}

func TestMessagesEndpoint_NonStreaming(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "Hello, how are you?"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify Anthropic format
	if resp["type"] != "message" {
		t.Errorf("expected type 'message', got %v", resp["type"])
	}
	if resp["role"] != "assistant" {
		t.Errorf("expected role 'assistant', got %v", resp["role"])
	}

	content, ok := resp["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("expected content blocks in response")
	}

	firstBlock, _ := content[0].(map[string]interface{})
	if firstBlock["type"] != "text" {
		t.Errorf("expected content type 'text', got %v", firstBlock["type"])
	}

	if resp["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("expected original model name, got %v", resp["model"])
	}

	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		t.Fatal("expected usage in response")
	}
	if usage["input_tokens"] == nil {
		t.Error("expected input_tokens in usage")
	}
}

func TestMessagesEndpoint_ValidationError(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{"invalid": "format"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", w.Code)
	}
}

func TestMessagesEndpoint_MissingFields(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{"model": "test"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", w.Code)
	}
}

func TestMessagesEndpoint_SystemMessage(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"system": "You are a helpful assistant",
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMessagesEndpoint_Tools(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "What's the weather?"}
		],
		"tools": [
			{
				"name": "get_weather",
				"description": "Get weather information",
				"input_schema": {
					"type": "object",
					"properties": {
						"location": {"type": "string"}
					},
					"required": ["location"]
				}
			}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMessagesEndpoint_InvalidAPIKey(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "wrong-key")

	w := httptest.NewRecorder()

	// Wrap with API key middleware
	mw := middleware.APIKeyMiddleware("your-proxy-api-key-here")
	handler := mw(http.HandlerFunc(msgHandler.ServeHTTP))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestMessagesEndpoint_AuthorizationBearer(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMessagesEndpoint_ModelNotFound(t *testing.T) {
	_, msgHandler := setupTestServer(t)

	// Remove __default__ route to test model-not-found
	cfg := config.GetConfig()
	delete(cfg.Routes, "__default__")

	body := `{
		"model": "unknown-model",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func strPtr(s string) *string {
	return &s
}
