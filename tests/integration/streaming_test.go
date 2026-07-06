package integration

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/major1201/anthropic-proxy-go/internal/config"
	"github.com/major1201/anthropic-proxy-go/internal/handler"
)

func setupStreamingTestServer(t *testing.T) (*httptest.Server, *handler.MessagesHandler) {
	t.Helper()

	mockOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		model, _ := req["model"].(string)

		// Send chunks
		chunks := []map[string]interface{}{
			{
				"id":      "chatcmpl-stream-123",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"role":    "assistant",
							"content": "",
						},
						"finish_reason": nil,
					},
				},
			},
			{
				"id":      "chatcmpl-stream-123",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"content": "Hello",
						},
						"finish_reason": nil,
					},
				},
			},
			{
				"id":      "chatcmpl-stream-123",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"content": " world",
						},
						"finish_reason": nil,
					},
				},
			},
			{
				"id":      "chatcmpl-stream-123",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"content": "",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":     10,
					"completion_tokens": 5,
					"total_tokens":      15,
				},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			w.Write([]byte("data: " + string(data) + "\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n"))
		flusher.Flush()
	}))
	t.Cleanup(mockOpenAI.Close)

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

func TestStreamingEndpoint(t *testing.T) {
	_, msgHandler := setupStreamingTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"stream": true,
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

	// Verify content type
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got %s", contentType)
	}

	// Parse SSE events
	events := parseSSEEvents(w.Body.String())

	if len(events) == 0 {
		t.Fatal("expected SSE events in response")
	}

	// Check for required event types
	eventTypes := make(map[string]bool)
	for _, evt := range events {
		eventTypes[evt.Event] = true
	}

	requiredEvents := []string{"message_start", "content_block_start", "content_block_delta", "message_delta", "message_stop"}
	for _, required := range requiredEvents {
		if !eventTypes[required] {
			t.Errorf("missing required event type: %s", required)
		}
	}
}

func TestStreamingEndpoint_ContentSequence(t *testing.T) {
	_, msgHandler := setupStreamingTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"stream": true,
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	events := parseSSEEvents(w.Body.String())

	// Verify event sequence
	if len(events) < 2 {
		t.Fatal("expected at least 2 events")
	}

	// First event should be message_start
	if events[0].Event != "message_start" {
		t.Errorf("expected message_start as first event, got %s", events[0].Event)
	}

	// Should have content_block_delta events
	foundDelta := false
	for _, evt := range events {
		if evt.Event == "content_block_delta" {
			foundDelta = true
			break
		}
	}
	if !foundDelta {
		t.Error("expected content_block_delta events")
	}
}

func TestStreamingEndpoint_UsageInMessageDelta(t *testing.T) {
	_, msgHandler := setupStreamingTestServer(t)

	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"stream": true,
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "your-proxy-api-key-here")

	w := httptest.NewRecorder()
	msgHandler.ServeHTTP(w, req)

	events := parseSSEEvents(w.Body.String())

	// Check message_delta has usage
	for _, evt := range events {
		if evt.Event == "message_delta" {
			var data map[string]interface{}
			json.Unmarshal([]byte(evt.Data), &data)
			if _, ok := data["usage"]; !ok {
				t.Error("expected usage in message_delta")
			}
			break
		}
	}
}

// SSEEvent represents a parsed SSE event.
type SSEEvent struct {
	Event string
	Data  string
}

func parseSSEEvents(body string) []SSEEvent {
	var events []SSEEvent
	scanner := bufio.NewScanner(strings.NewReader(body))

	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			events = append(events, SSEEvent{Event: currentEvent, Data: data})
			currentEvent = ""
		}
	}
	return events
}
