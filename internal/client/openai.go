// Package client provides an HTTP client for OpenAI API communication.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/major1201/anthropic-proxy-go/internal/common"
	"github.com/major1201/anthropic-proxy-go/internal/models"
)

// ClientError wraps a StandardErrorResponse as an error.
type ClientError struct {
	ErrorResponse *models.StandardErrorResponse
}

func (e *ClientError) Error() string {
	data, _ := json.Marshal(e.ErrorResponse)
	return string(data)
}

// OpenAIClient is an HTTP client for OpenAI-compatible APIs with connection pooling.
type OpenAIClient struct {
	httpClient *http.Client
}

// NewOpenAIClient creates a new OpenAIClient with the specified timeout.
func NewOpenAIClient(timeout time.Duration) *OpenAIClient {
	return &OpenAIClient{
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// SendRequest sends a non-streaming request to the OpenAI API.
func (c *OpenAIClient) SendRequest(ctx context.Context, baseURL, apiKey string, req *models.OpenAIRequest, requestID string) (map[string]interface{}, error) {
	logger := common.SafeLogger(requestID)

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.Info("sending OpenAI request",
		"url", url,
		"model", req.Model,
		"message_count", len(req.Messages),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logger.Error("OpenAI API request failed", "error", err)
		return nil, &ClientError{
			ErrorResponse: models.GetErrorResponse(502, err.Error(), map[string]interface{}{
				"type": "connection_error",
			}),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{
			ErrorResponse: models.GetErrorResponse(502, err.Error(), nil),
		}
	}

	logger.Info("received OpenAI response",
		"status", resp.StatusCode,
		"content_type", resp.Header.Get("content-type"),
		"size", len(respBody),
	)

	if resp.StatusCode >= 400 {
		return nil, &ClientError{
			ErrorResponse: models.GetErrorResponse(resp.StatusCode, string(respBody), map[string]interface{}{
				"type": "http_error",
			}),
		}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		logger.Error("OpenAI JSON parse failed",
			"status", resp.StatusCode,
			"error", err,
			"preview", truncateStr(string(respBody), 500),
		)
		return nil, &ClientError{
			ErrorResponse: models.GetErrorResponse(502, fmt.Sprintf("failed to parse response: %v", err), nil),
		}
	}

	return result, nil
}

// SendStreamingRequest sends a streaming request to the OpenAI API.
// Returns a channel of raw SSE data lines.
func (c *OpenAIClient) SendStreamingRequest(ctx context.Context, baseURL, apiKey string, req *models.OpenAIRequest, requestID string) (<-chan string, error) {
	logger := common.SafeLogger(requestID)

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"

	// Ensure streaming is enabled
	stream := true
	req.Stream = &stream

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.Info("sending OpenAI streaming request",
		"url", url,
		"model", req.Model,
		"message_count", len(req.Messages),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logger.Error("OpenAI streaming request failed", "error", err)
		return nil, &ClientError{
			ErrorResponse: models.GetErrorResponse(502, err.Error(), map[string]interface{}{
				"type": "connection_error",
			}),
		}
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, &ClientError{
			ErrorResponse: models.GetErrorResponse(resp.StatusCode, string(errBody), map[string]interface{}{
				"type": "http_error",
			}),
		}
	}

	ch := make(chan string, 64)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if line != "" {
				ch <- line
				if line == "data: [DONE]" {
					return
				}
			}
		}
	}()

	return ch, nil
}

// HealthCheck checks the availability of the OpenAI API.
func (c *OpenAIClient) HealthCheck(ctx context.Context, baseURL string) map[string]interface{} {
	url := strings.TrimRight(baseURL, "/") + "/models"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return map[string]interface{}{
			"openai_service": false,
			"api_accessible": false,
			"error":          err.Error(),
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.Error("OpenAI health check failed", "error", err)
		return map[string]interface{}{
			"openai_service": false,
			"api_accessible": false,
		}
	}
	defer resp.Body.Close()

	return map[string]interface{}{
		"openai_service": resp.StatusCode == 200,
		"api_accessible": true,
		"last_check":     true,
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
