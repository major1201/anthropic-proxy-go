// Package converter handles SSE streaming response conversion.
package converter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/major1201/anthropic-proxy-go/internal/common"
	"github.com/major1201/anthropic-proxy-go/internal/models"
)

// SSEEvent represents a single SSE event.
type SSEEvent struct {
	Event string
	Data  string
}

// FormatSSE formats an SSEEvent as an SSE string.
func (e SSEEvent) FormatSSE() string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Event, e.Data)
}

// StreamState tracks the state of a streaming conversion.
type StreamState struct {
	MessageID                     string
	HasStarted                    bool
	ContentStarted                bool
	HasTextContentStarted         bool
	HasFinished                   bool
	ThinkingStarted               bool
	ThinkingFinished              bool
	ContentIndex                  int
	Buffer                        string
	ThinkingMode                  *int // nil=off, 1=thinking tag, 2=reasoning_content
	TotalChunks                   int
	ToolCallChunks                int
	ToolCalls                     map[int]*ToolCallInfo
	ToolCallIndexToContentBlock   map[int]int
	AccumulatedContent            []string
	Usage                         *models.AnthropicUsage
	AnthropicStopReason           string
}

// ToolCallInfo tracks a single tool call during streaming.
type ToolCallInfo struct {
	ID               string
	Name             string
	Arguments        string
	ContentBlockIndex int
}

// NewStreamState creates a new StreamState.
func NewStreamState() *StreamState {
	return &StreamState{
		MessageID:                   fmt.Sprintf("msg_%d", time.Now().UnixMilli()),
		ToolCalls:                   make(map[int]*ToolCallInfo),
		ToolCallIndexToContentBlock: make(map[int]int),
	}
}

var stopReasonMap = map[string]string{
	"stop":          "end_turn",
	"length":        "max_tokens",
	"tool_calls":    "tool_use",
	"content_filter": "stop_sequence",
}

// ConvertStream converts an OpenAI SSE stream to Anthropic SSE events.
func (rc *ResponseConverter) ConvertStream(ctx context.Context, reader io.Reader, model string, requestID string) <-chan SSEEvent {
	ch := make(chan SSEEvent, 64)

	go func() {
		defer close(ch)

		logger := common.SafeLogger(requestID)
		state := NewStreamState()
		scanner := bufio.NewScanner(reader)

		for scanner.Scan() {
			if state.HasFinished {
				break
			}

			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}

			var chunkData map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunkData); err != nil {
				logger.Error("failed to parse streaming chunk", "error", err, "data", truncate(data, 100))
				continue
			}

			state.TotalChunks++

			// Handle errors in stream
			if _, hasError := chunkData["error"]; hasError {
				errorData, _ := json.Marshal(chunkData["error"])
				ch <- SSEEvent{
					Event: "error",
					Data:  fmt.Sprintf(`{"type":"error","message":{"type":"api_error","message":%s}}`, string(errorData)),
				}
				continue
			}

			// Send message_start on first chunk
			if !state.HasStarted && !state.HasFinished {
				inputTokens := 0
				if cached, ok := common.GetCachedTokens(requestID, false); ok {
					inputTokens = cached
				}
				state.HasStarted = true

				startMsg := map[string]interface{}{
					"type": models.EventMessageStart,
					"message": map[string]interface{}{
						"id":    state.MessageID,
						"type":  models.MessageTypeMessage,
						"role":  models.RoleAssistant,
						"model": model,
						"content": []interface{}{},
						"usage": map[string]interface{}{
							"input_tokens": inputTokens,
						},
					},
				}
				data, _ := json.Marshal(startMsg)
				ch <- SSEEvent{Event: models.EventMessageStart, Data: string(data)}
			}

			choicesRaw, _ := chunkData["choices"].([]interface{})
			if len(choicesRaw) == 0 {
				continue
			}

			choice, _ := choicesRaw[0].(map[string]interface{})
			delta, _ := choice["delta"].(map[string]interface{})
			if delta == nil {
				continue
			}

			content, _ := delta["content"].(string)
			reasoningContent, _ := delta["reasoning_content"].(string)
			toolCallsRaw := delta["tool_calls"]

			hasContent := content != ""
			hasReasoning := reasoningContent != ""
			hasToolCalls := toolCallsRaw != nil

			if !hasContent && !hasReasoning && !hasToolCalls {
				if finishReason, _ := choice["finish_reason"].(string); finishReason == "" {
					continue
				}
			}

			// Process thinking content
			for _, evt := range processThinkingContent(delta, state) {
				ch <- evt
			}

			// Process regular text content
			for _, evt := range processRegularContent(delta, state) {
				ch <- evt
			}

			// Process tool calls
			if hasToolCalls {
				for _, evt := range processToolCalls(delta, state) {
					ch <- evt
				}
			}

			// Process finish event
			finishReason, _ := choice["finish_reason"].(string)
			if finishReason != "" {
				for _, evt := range processFinishEvent(chunkData, state, requestID) {
					ch <- evt
				}

				// Log completion
				logStreamCompletion(state, requestID, model, logger)
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Error("stream scan error", "error", err)
		}
	}()

	return ch
}

func checkThinkingContent(delta map[string]interface{}, state *StreamState) bool {
	if state.ThinkingMode != nil {
		return true
	}
	content, _ := delta["content"].(string)
	if strings.Contains(content, " thinking") || strings.Contains(content, "<thinking>") {
		mode := 1
		state.ThinkingMode = &mode
		return true
	}
	reasoningContent, _ := delta["reasoning_content"].(string)
	if reasoningContent != "" {
		mode := 2
		state.ThinkingMode = &mode
		return true
	}
	return false
}

func checkRegularContent(delta map[string]interface{}, state *StreamState) bool {
	if state.ThinkingMode != nil {
		return false
	}
	content, _ := delta["content"].(string)
	return content != ""
}

func processRegularContent(delta map[string]interface{}, state *StreamState) []SSEEvent {
	var events []SSEEvent

	if !checkRegularContent(delta, state) {
		return events
	}

	if !state.ContentStarted {
		state.ContentStarted = true
		state.HasTextContentStarted = true

		start := models.AnthropicStreamContentBlockStart{
			Type:  models.EventContentBlockStart,
			Index: state.ContentIndex,
			ContentBlock: models.ContentBlock{
				Type: models.ContentTypeText,
				Text: strPtr(""),
			},
		}
		data, _ := json.Marshal(start)
		events = append(events, SSEEvent{Event: models.EventContentBlockStart, Data: string(data)})

		ping := models.AnthropicPing{Type: models.EventPing}
		pingData, _ := json.Marshal(ping)
		events = append(events, SSEEvent{Event: models.EventPing, Data: string(pingData)})
	}

	content, _ := delta["content"].(string)
	if content != "" {
		state.AccumulatedContent = append(state.AccumulatedContent, content)
	}

	chunk := models.AnthropicStreamContentBlock{
		Type:  models.EventContentBlockDelta,
		Index: state.ContentIndex,
		Delta: &models.Delta{
			Type: models.ContentTypeTextDelta,
			Text: &content,
		},
	}
	data, _ := json.Marshal(chunk)
	events = append(events, SSEEvent{Event: models.EventContentBlockDelta, Data: string(data)})

	return events
}

func processThinkingContent(delta map[string]interface{}, state *StreamState) []SSEEvent {
	var events []SSEEvent

	isThinking := checkThinkingContent(delta, state)

	if !state.ThinkingStarted && isThinking {
		state.ThinkingStarted = true

		start := models.AnthropicStreamContentBlockStart{
			Type:  models.EventContentBlockStart,
			Index: state.ContentIndex,
			ContentBlock: models.ContentBlock{
				Type:     models.ContentTypeThinking,
				Thinking: strPtr(""),
			},
		}
		data, _ := json.Marshal(start)
		events = append(events, SSEEvent{Event: models.EventContentBlockStart, Data: string(data)})

		ping := models.AnthropicPing{Type: models.EventPing}
		pingData, _ := json.Marshal(ping)
		events = append(events, SSEEvent{Event: models.EventPing, Data: string(pingData)})
	}

	// Extract thinking content
	var thinkingContent string
	if state.ThinkingMode != nil {
		switch *state.ThinkingMode {
		case 1:
			content, _ := delta["content"].(string)
			if strings.Contains(content, " response") || strings.Contains(content, "</thinking>") {
				state.ThinkingMode = nil
			}
			thinkingContent = strings.ReplaceAll(strings.ReplaceAll(content, " thinking", ""), " response", "")
		case 2:
			thinkingContent, _ = delta["reasoning_content"].(string)
		}
	}

	if thinkingContent != "" {
		state.AccumulatedContent = append(state.AccumulatedContent, thinkingContent)

		chunk := models.AnthropicStreamContentBlock{
			Type:  models.EventContentBlockDelta,
			Index: state.ContentIndex,
			Delta: &models.Delta{
				Type:     models.ContentTypeThinkingDelta,
				Thinking: &thinkingContent,
			},
		}
		data, _ := json.Marshal(chunk)
		events = append(events, SSEEvent{Event: models.EventContentBlockDelta, Data: string(data)})
	}

	if thinkingContent == "" && state.ThinkingStarted && !state.ThinkingFinished {
		state.ThinkingMode = nil
		state.ThinkingFinished = true

		// Signature delta
		sig := fmt.Sprintf("%d", time.Now().UnixMilli())
		sigDelta := models.AnthropicStreamContentBlock{
			Type:  models.EventContentBlockDelta,
			Index: state.ContentIndex,
			Delta: &models.Delta{
				Type:      models.ContentTypeSignatureDelta,
				Signature: &sig,
			},
		}
		sigData, _ := json.Marshal(sigDelta)
		events = append(events, SSEEvent{Event: models.EventContentBlockDelta, Data: string(sigData)})

		// Content block stop
		stop := models.AnthropicStreamContentBlockStop{
			Type:  models.EventContentBlockStop,
			Index: state.ContentIndex,
		}
		stopData, _ := json.Marshal(stop)
		events = append(events, SSEEvent{Event: models.EventContentBlockStop, Data: string(stopData)})

		state.ContentIndex++
	}

	return events
}

func processToolCalls(delta map[string]interface{}, state *StreamState) []SSEEvent {
	var events []SSEEvent
	state.ToolCallChunks++

	toolCallsRaw := delta["tool_calls"]
	toolCalls, ok := toolCallsRaw.([]interface{})
	if !ok {
		return events
	}

	processedIndices := make(map[int]bool)

	for _, tcRaw := range toolCalls {
		tc, _ := tcRaw.(map[string]interface{})

		idx := 0
		if index, ok := tc["index"].(float64); ok {
			idx = int(index)
		}

		if processedIndices[idx] {
			continue
		}
		processedIndices[idx] = true

		// Handle new tool call
		if _, exists := state.ToolCallIndexToContentBlock[idx]; !exists {
			newContentBlockIdx := len(state.ToolCallIndexToContentBlock)
			if state.HasTextContentStarted {
				newContentBlockIdx = len(state.ToolCallIndexToContentBlock) + 1
			}

			if newContentBlockIdx != 0 {
				stop := models.AnthropicStreamContentBlockStop{
					Type:  models.EventContentBlockStop,
					Index: state.ContentIndex,
				}
				stopData, _ := json.Marshal(stop)
				events = append(events, SSEEvent{Event: models.EventContentBlockStop, Data: string(stopData)})
				state.ContentIndex++
			}

			state.ToolCallIndexToContentBlock[idx] = newContentBlockIdx

			toolCallID, _ := tc["id"].(string)
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), idx)
			}

			toolCallName := fmt.Sprintf("tool_%d", idx)
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok {
					toolCallName = name
				}
			}

			if !strings.HasPrefix(toolCallName, "tool_") {
				state.AccumulatedContent = append(state.AccumulatedContent, toolCallName)
			}

			start := models.AnthropicStreamContentBlockStart{
				Type:  models.EventContentBlockStart,
				Index: state.ContentIndex,
				ContentBlock: models.ContentBlock{
					Type:  models.ContentTypeToolUse,
					ID:    &toolCallID,
					Name:  &toolCallName,
					Input: json.RawMessage(`{}`),
				},
			}
			startData, _ := json.Marshal(start)
			events = append(events, SSEEvent{Event: models.EventContentBlockStart, Data: string(startData)})

			ping := models.AnthropicPing{Type: models.EventPing}
			pingData, _ := json.Marshal(ping)
			events = append(events, SSEEvent{Event: models.EventPing, Data: string(pingData)})

			state.ToolCalls[idx] = &ToolCallInfo{
				ID:               toolCallID,
				Name:             toolCallName,
				Arguments:        "",
				ContentBlockIndex: newContentBlockIdx,
			}
		} else if existing, ok := state.ToolCalls[idx]; ok {
			// Update existing tool call with real ID/name
			if id, ok := tc["id"].(string); ok {
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok {
						wasTemporary := strings.HasPrefix(existing.ID, "call_") && strings.HasPrefix(existing.Name, "tool_")
						if wasTemporary {
							existing.ID = id
							existing.Name = name
						}
					}
				}
			}
		}

		// Process tool call arguments
		var functionArgs string
		if fn, ok := tc["function"].(map[string]interface{}); ok {
			if args, ok := fn["arguments"].(string); ok {
				functionArgs = args
			}
		}

		if functionArgs != "" && !state.HasFinished {
			state.AccumulatedContent = append(state.AccumulatedContent, functionArgs)

			if tci, ok := state.ToolCalls[idx]; ok {
				tci.Arguments += functionArgs
			}

			chunk := models.AnthropicStreamContentBlock{
				Type:  models.EventContentBlockDelta,
				Index: state.ContentIndex,
				Delta: &models.Delta{
					Type:        models.ContentTypeInputJSONDelta,
					PartialJSON: &functionArgs,
				},
			}
			data, _ := json.Marshal(chunk)
			events = append(events, SSEEvent{Event: models.EventContentBlockDelta, Data: string(data)})
		}
	}

	return events
}

func processFinishEvent(chunkData map[string]interface{}, state *StreamState, requestID string) []SSEEvent {
	var events []SSEEvent
	state.HasFinished = true

	// End the last content block
	stop := models.AnthropicStreamContentBlockStop{
		Type:  models.EventContentBlockStop,
		Index: state.ContentIndex,
	}
	stopData, _ := json.Marshal(stop)
	events = append(events, SSEEvent{Event: models.EventContentBlockStop, Data: string(stopData)})

	choicesRaw, _ := chunkData["choices"].([]interface{})
	var choice map[string]interface{}
	if len(choicesRaw) > 0 {
		choice, _ = choicesRaw[0].(map[string]interface{})
	}

	finishReason, _ := choice["finish_reason"].(string)
	anthropicStopReason := stopReasonMap[finishReason]
	if anthropicStopReason == "" {
		anthropicStopReason = "end_turn"
	}
	state.AnthropicStopReason = anthropicStopReason

	// Extract usage
	usageData, _ := choice["usage"].(map[string]interface{})
	if usageData == nil {
		usageData, _ = chunkData["usage"].(map[string]interface{})
	}
	if usageData == nil {
		usageData = make(map[string]interface{})
	}

	inputTokens := 0
	if pt, ok := usageData["prompt_tokens"].(float64); ok {
		inputTokens = int(pt)
	}
	if inputTokens == 0 {
		if cached, ok := common.GetCachedTokens(requestID, true); ok {
			inputTokens = cached
		}
	}

	completionTokens := 0
	if ct, ok := usageData["completion_tokens"].(float64); ok {
		completionTokens = int(ct)
	}
	if completionTokens == 0 && len(state.AccumulatedContent) > 0 {
		combinedText := strings.Join(state.AccumulatedContent, "")
		completionTokens, _ = common.GlobalCounter.CountResponseTokens([]models.AnthropicContentBlock{
			{Type: models.ContentTypeText, Text: &combinedText},
		})
	}

	usage := models.AnthropicUsage{
		InputTokens:  inputTokens,
		OutputTokens: &completionTokens,
	}
	state.Usage = &usage

	// Send message_delta
	delta := models.AnthropicStreamMessage{
		Type: models.EventMessageDelta,
		Delta: &models.MessageDelta{
			StopReason: &anthropicStopReason,
		},
		Usage: &usage,
	}
	deltaData, _ := json.Marshal(delta)
	events = append(events, SSEEvent{Event: models.EventMessageDelta, Data: string(deltaData)})

	// Send message_stop
	msgStop := models.AnthropicStreamMessage{
		Type: models.EventMessageStop,
	}
	msgStopData, _ := json.Marshal(msgStop)
	events = append(events, SSEEvent{Event: models.EventMessageStop, Data: string(msgStopData)})

	return events
}

func logStreamCompletion(state *StreamState, requestID string, model string, logger *slog.Logger) {
	if state.Usage == nil {
		return
	}

	responseJSON := buildCompleteResponse(state, model)
	formatted, _ := json.MarshalIndent(responseJSON, "", "  ")
	logger.Info("streaming response generation complete", "response", string(formatted))
}

func buildCompleteResponse(state *StreamState, model string) map[string]interface{} {
	var contentBlocks []map[string]interface{}

	// Thinking content
	if state.ThinkingStarted {
		var thinkingText string
		for _, content := range state.AccumulatedContent {
			if strings.Contains(content, " thinking") || strings.Contains(content, " response") ||
				strings.Contains(content, "Let me think") || strings.Contains(content, "I need to think") {
				thinkingText += content
			}
		}

		cleanThinking := strings.TrimSpace(
			strings.ReplaceAll(strings.ReplaceAll(thinkingText, " thinking", ""), " response", ""),
		)
		if cleanThinking != "" {
			contentBlocks = append(contentBlocks, map[string]interface{}{
				"type":      models.ContentTypeThinking,
				"thinking":  cleanThinking,
				"signature": fmt.Sprintf("%d", time.Now().UnixMilli()),
			})
		}
	}

	// Text content
	if state.ContentStarted {
		var textContent string
		for _, content := range state.AccumulatedContent {
			if !strings.Contains(content, " thinking") && !strings.Contains(content, " response") &&
				!strings.Contains(content, "Let me think") && !strings.Contains(content, "I need to think") {
				if !(strings.HasPrefix(strings.TrimSpace(content), "{") && strings.HasSuffix(strings.TrimSpace(content), "}")) {
					toolNames := []string{"search", "calculate", "web_search", "tool_"}
					isTool := false
					for _, tn := range toolNames {
						if strings.Contains(strings.ToLower(content), tn) {
							isTool = true
							break
						}
					}
					if !isTool {
						textContent += content
					}
				}
			}
		}

		if strings.TrimSpace(textContent) != "" {
			contentBlocks = append(contentBlocks, map[string]interface{}{
				"type": models.ContentTypeText,
				"text": strings.TrimSpace(textContent),
			})
		}
	}

	// Tool calls
	for idx, tci := range state.ToolCalls {
		toolInput := safeJSONParse(tci.Arguments)
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type":  models.ContentTypeToolUse,
			"id":    tci.ID,
			"name":  tci.Name,
			"input": toolInput,
		})
		_ = idx // suppress unused warning
	}

	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": models.ContentTypeText,
			"text": "",
		})
	}

	usage := state.Usage
	if usage == nil {
		usage = &models.AnthropicUsage{}
	}

	return map[string]interface{}{
		"id":      state.MessageID,
		"type":    models.MessageTypeMessage,
		"role":    models.RoleAssistant,
		"content": contentBlocks,
		"model":   model,
		"stop_reason": state.AnthropicStopReason,
		"usage": map[string]interface{}{
			"input_tokens":               usage.InputTokens,
			"output_tokens":              usage.OutputTokens,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":    0,
			"service_tier":               "standard",
		},
	}
}

func safeJSONParse(jsonStr string) json.RawMessage {
	if jsonStr == "" {
		return json.RawMessage(`{}`)
	}

	var result interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Try replacing single quotes
		corrected := strings.ReplaceAll(jsonStr, "'", "\"")
		if err := json.Unmarshal([]byte(corrected), &result); err != nil {
			slog.Warn("JSON parse failed, using empty object", "error", err, "content", truncate(jsonStr, 100))
			return json.RawMessage(`{}`)
		}
	}

	data, _ := json.Marshal(result)
	return data
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
