// Package converter handles conversion between Anthropic and OpenAI API formats.
package converter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/major1201/anthropic-proxy-go/internal/common"
	"github.com/major1201/anthropic-proxy-go/internal/config"
	"github.com/major1201/anthropic-proxy-go/internal/models"
)

// RequestConverter converts Anthropic requests to OpenAI format.
type RequestConverter struct{}

// NewRequestConverter creates a new RequestConverter.
func NewRequestConverter() *RequestConverter {
	return &RequestConverter{}
}

// GetTargetModel determines the target OpenAI model for a given Anthropic model name.
func (rc *RequestConverter) GetTargetModel(anthropicModel string) (string, error) {
	if anthropicModel != "" && containsComma(anthropicModel) {
		return anthropicModel, nil
	}

	cfg := config.GetConfig()
	route, ok := cfg.GetRoute(anthropicModel)
	if !ok {
		return "", fmt.Errorf("model mapping not found for model: %s", anthropicModel)
	}
	if route.Model != nil {
		return *route.Model, nil
	}
	return anthropicModel, nil
}

func containsComma(s string) bool {
	for _, c := range s {
		if c == ',' {
			return true
		}
	}
	return false
}

// Convert converts an Anthropic request to an OpenAI request.
func (rc *RequestConverter) Convert(ctx context.Context, req *models.AnthropicRequest, requestID string) (*models.OpenAIRequest, error) {
	logger := common.SafeLogger(requestID)

	targetModel, err := rc.GetTargetModel(req.Model)
	if err != nil {
		return nil, err
	}

	logger.Debug("converting Anthropic request to OpenAI format",
		"source_model", req.Model,
		"target_model", targetModel,
		"message_count", len(req.Messages),
		"has_tools", req.Tools != nil,
		"has_system", len(req.System) > 0,
	)

	// Token counting
	counter := common.GlobalCounter
	totalTokens, err := counter.CountTokens(req.Messages, parseSystemRaw(req.System), req.Tools)
	if err != nil {
		logger.Warn("token counting failed", "error", err)
	} else if requestID != "" {
		common.CacheTokens(requestID, totalTokens)
	}

	// Convert messages
	messages := convertMessages(req)

	// Convert tools
	tools := convertTools(req.Tools)

	// Apply parameter overrides
	cfg := config.GetConfig()
	overrides := cfg.ParameterOverrides

	finalMaxTokens := req.MaxTokens
	if overrides.MaxTokens != nil {
		finalMaxTokens = *overrides.MaxTokens
		logger.Debug("parameter override", "max_tokens", fmt.Sprintf("%d -> %d", req.MaxTokens, finalMaxTokens))
	}

	var finalTemperature *float64
	if overrides.Temperature != nil {
		finalTemperature = overrides.Temperature
	} else {
		finalTemperature = req.Temperature
	}

	var finalTopP *float64
	if overrides.TopP != nil {
		finalTopP = overrides.TopP
	} else {
		finalTopP = req.TopP
	}

	var finalTopK *int
	if overrides.TopK != nil {
		finalTopK = overrides.TopK
	} else {
		finalTopK = req.TopK
	}

	// Build OpenAI request
	openaiReq := &models.OpenAIRequest{
		Model:      targetModel,
		Messages:   messages,
		MaxTokens:  &finalMaxTokens,
		Temperature: finalTemperature,
		TopP:       finalTopP,
		TopK:       finalTopK,
		Stream:     req.Stream,
		Stop:       marshalStopSequences(req.StopSequences),
		Tools:      tools,
		ToolChoice: convertToolChoice(req.ToolChoice),
	}

	logger.Info("model conversion complete",
		"anthropic_model", req.Model,
		"openai_model", openaiReq.Model,
	)

	return openaiReq, nil
}

// parseSystemRaw attempts to parse the system field as string or array.
func parseSystemRaw(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}

	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of system messages
	var msgs []interface{}
	if err := json.Unmarshal(raw, &msgs); err == nil {
		return msgs
	}

	return nil
}

func marshalStopSequences(stop []string) json.RawMessage {
	if len(stop) == 0 {
		return nil
	}
	data, _ := json.Marshal(stop)
	return data
}

// convertMessages converts Anthropic messages to OpenAI format.
func convertMessages(req *models.AnthropicRequest) []models.OpenAIMessage {
	var messages []models.OpenAIMessage

	// Process system message
	systemMsgs := convertSystemMessage(req.System)
	messages = append(messages, systemMsgs...)

	// Convert user/assistant messages
	for _, msg := range req.Messages {
		converted := convertSingleMessage(msg)
		messages = append(messages, converted...)
	}

	// Filter incomplete tool call sequences
	messages = filterIncompleteToolCalls(messages)

	return messages
}

// convertSystemMessage converts Anthropic system to OpenAI system messages.
func convertSystemMessage(system json.RawMessage) []models.OpenAIMessage {
	if len(system) == 0 {
		return nil
	}

	// Try string
	var sysText string
	if err := json.Unmarshal(system, &sysText); err == nil {
		content, _ := json.Marshal(sysText)
		return []models.OpenAIMessage{{Role: "system", Content: content}}
	}

	// Try array of system messages
	var sysMsgs []models.AnthropicSystemMessage
	if err := json.Unmarshal(system, &sysMsgs); err == nil {
		var messages []models.OpenAIMessage
		for _, sm := range sysMsgs {
			content, _ := json.Marshal(sm.Text)
			messages = append(messages, models.OpenAIMessage{Role: "system", Content: content})
		}
		return messages
	}

	return nil
}

// convertSingleMessage converts a single Anthropic message to OpenAI format.
func convertSingleMessage(msg models.AnthropicMessage) []models.OpenAIMessage {
	if len(msg.Content) == 0 {
		return nil
	}

	// Try string content
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		content, _ := json.Marshal(textContent)
		return []models.OpenAIMessage{{Role: msg.Role, Content: content}}
	}

	// Try array of content blocks
	var contentBlocks []map[string]interface{}
	if err := json.Unmarshal(msg.Content, &contentBlocks); err != nil {
		content, _ := json.Marshal(string(msg.Content))
		return []models.OpenAIMessage{{Role: msg.Role, Content: content}}
	}

	var contentParts []map[string]interface{}
	var toolCalls []map[string]interface{}
	var toolResults []map[string]interface{}

	for _, block := range contentBlocks {
		blockType, _ := block["type"].(string)
		switch blockType {
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input := block["input"]
			argsJSON, _ := json.Marshal(input)

			toolCall := map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(argsJSON),
				},
			}
			toolCalls = append(toolCalls, toolCall)

		case "tool_result":
			toolUseID, _ := block["tool_use_id"].(string)
			resultContent := block["content"]

			switch v := resultContent.(type) {
			case []interface{}:
				data, _ := json.Marshal(v)
				resultContent = string(data)
			case string:
				resultContent = v
			}

			toolResults = append(toolResults, map[string]interface{}{
				"tool_call_id": toolUseID,
				"content":      resultContent,
			})

		case "text", "image_url":
			contentParts = append(contentParts, block)
		}
	}

	// If there are tool_results, return multiple messages
	if len(toolResults) > 0 {
		var messages []models.OpenAIMessage

		if len(contentParts) > 0 || len(toolCalls) > 0 {
			mainMsg := buildAssistantMessage(msg.Role, contentParts, toolCalls)
			messages = append(messages, mainMsg)
		}

		for _, tr := range toolResults {
			toolCallID, _ := tr["tool_call_id"].(string)
			content := tr["content"]
			contentJSON, _ := json.Marshal(content)
			messages = append(messages, models.OpenAIMessage{
				Role:       "tool",
				Content:    contentJSON,
				ToolCallID: &toolCallID,
			})
		}

		return messages
	}

	// Single message
	msg_ := buildAssistantMessage(msg.Role, contentParts, toolCalls)
	return []models.OpenAIMessage{msg_}
}

func buildAssistantMessage(role string, contentParts []map[string]interface{}, toolCalls []map[string]interface{}) models.OpenAIMessage {
	msg := models.OpenAIMessage{Role: role}

	if len(contentParts) > 0 {
		if len(contentParts) == 1 && contentParts[0]["type"] == "text" {
			if text, ok := contentParts[0]["text"].(string); ok {
				content, _ := json.Marshal(text)
				msg.Content = content
			}
		} else {
			content, _ := json.Marshal(contentParts)
			msg.Content = content
		}
	}

	if len(toolCalls) > 0 {
		tcJSON, _ := json.Marshal(toolCalls)
		msg.ToolCalls = tcJSON
	}

	return msg
}

// convertTools converts Anthropic tool definitions to OpenAI format.
func convertTools(anthropicTools []models.AnthropicToolDefinition) []models.OpenAITool {
	if len(anthropicTools) == 0 {
		return nil
	}

	// Check for web_search
	for _, tool := range anthropicTools {
		if tool.Type != nil && contains(tool.Type, "web_search") {
			return []models.OpenAITool{{
				Type: "function",
				Function: models.OpenAIToolFunction{
					Name: "googleSearch",
				},
			}}
		}
	}

	var tools []models.OpenAITool
	for _, tool := range anthropicTools {
		var desc *string
		if tool.Description != nil {
			desc = tool.Description
		}
		tools = append(tools, models.OpenAITool{
			Type: "function",
			Function: models.OpenAIToolFunction{
				Name:        tool.Name,
				Description: desc,
				Parameters:  tool.InputSchema,
			},
		})
	}

	if len(tools) == 0 {
		return nil
	}
	return tools
}

// convertToolChoice converts Anthropic tool_choice to OpenAI format.
func convertToolChoice(toolChoice json.RawMessage) json.RawMessage {
	if len(toolChoice) == 0 {
		return nil
	}

	// Try string
	var s string
	if err := json.Unmarshal(toolChoice, &s); err == nil {
		switch s {
		case "any":
			data, _ := json.Marshal("required")
			return data
		case "auto":
			data, _ := json.Marshal("auto")
			return data
		default:
			return toolChoice
		}
	}

	// Try object
	var obj map[string]interface{}
	if err := json.Unmarshal(toolChoice, &obj); err == nil {
		if obj["type"] == "tool" {
			if name, ok := obj["name"].(string); ok {
				result := map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name": name,
					},
				}
				data, _ := json.Marshal(result)
				return data
			}
		}
	}

	return toolChoice
}

// filterIncompleteToolCalls removes assistant tool_calls without corresponding tool messages.
func filterIncompleteToolCalls(messages []models.OpenAIMessage) []models.OpenAIMessage {
	if len(messages) == 0 {
		return messages
	}

	var filtered []models.OpenAIMessage
	i := 0

	for i < len(messages) {
		msg := messages[i]

		// If assistant with tool_calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var calls []map[string]interface{}
			if err := json.Unmarshal(msg.ToolCalls, &calls); err == nil {
				toolCallIDs := make(map[string]bool)
				for _, call := range calls {
					if id, ok := call["id"].(string); ok {
						toolCallIDs[id] = true
					}
				}

				foundIDs := make(map[string]bool)
				j := i + 1
				for j < len(messages) && messages[j].Role == "tool" {
					if messages[j].ToolCallID != nil && toolCallIDs[*messages[j].ToolCallID] {
						foundIDs[*messages[j].ToolCallID] = true
					}
					j++
				}

				if len(foundIDs) == len(toolCallIDs) {
					filtered = append(filtered, msg)
					for k := i + 1; k < j; k++ {
						if messages[k].Role == "tool" {
							filtered = append(filtered, messages[k])
						}
					}
					i = j
				} else {
					slog.Debug("filtering incomplete tool_calls sequence",
						"expected", len(toolCallIDs),
						"found", len(foundIDs),
					)
					i = j
				}
			} else {
				filtered = append(filtered, msg)
				i++
			}
		} else if msg.Role == "tool" {
			// Standalone tool message - check for preceding assistant
			hasCorresponding := false
			for k := i - 1; k >= 0; k-- {
				prev := messages[k]
				if prev.Role == "assistant" && len(prev.ToolCalls) > 0 {
					var calls []map[string]interface{}
					if err := json.Unmarshal(prev.ToolCalls, &calls); err == nil {
						for _, call := range calls {
							if id, ok := call["id"].(string); ok {
								if msg.ToolCallID != nil && *msg.ToolCallID == id {
									hasCorresponding = true
									break
								}
							}
						}
					}
					if hasCorresponding {
						break
					}
				} else if prev.Role != "tool" {
					break
				}
			}

			if hasCorresponding {
				filtered = append(filtered, msg)
			} else {
				slog.Debug("filtering standalone tool message",
					"tool_call_id", msg.ToolCallID,
				)
			}
			i++
		} else {
			filtered = append(filtered, msg)
			i++
		}
	}

	return filtered
}

func contains(s *string, substr string) bool {
	if s == nil {
		return false
	}
	for i := 0; i < len(*s); i++ {
		if i+len(substr) <= len(*s) && (*s)[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
