// Package converter handles OpenAI to Anthropic response conversion.
package converter

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/major1201/anthropic-proxy-go/internal/common"
	"github.com/major1201/anthropic-proxy-go/internal/models"
)

// ResponseConverter converts OpenAI responses to Anthropic format.
type ResponseConverter struct{}

// NewResponseConverter creates a new ResponseConverter.
func NewResponseConverter() *ResponseConverter {
	return &ResponseConverter{}
}

var finishReasonMap = map[string]string{
	"stop":          "end_turn",
	"length":        "max_tokens",
	"content_filter": "content_filter",
	"tool_calls":    "tool_use",
	"function_call": "tool_use",
}

// ConvertResponse converts an OpenAI non-streaming response to Anthropic format.
func (rc *ResponseConverter) ConvertResponse(openaiResp map[string]interface{}, originalModel string, requestID string) (*models.AnthropicMessageResponse, error) {
	choicesRaw, ok := openaiResp["choices"].([]interface{})
	if !ok || len(choicesRaw) == 0 {
		return nil, fmt.Errorf("OpenAI response has no valid choices")
	}

	firstChoice, _ := choicesRaw[0].(map[string]interface{})

	// Extract content blocks
	contentBlocks := extractContentBlocksWithReasoning(firstChoice)

	// Convert usage
	usage := convertUsage(openaiResp, requestID, contentBlocks)

	// Determine model
	model := originalModel
	if model == "" {
		if m, ok := openaiResp["model"].(string); ok {
			model = m
		}
	}

	// Map finish reason
	finishReason, _ := firstChoice["finish_reason"].(string)
	stopReason := finishReasonMap[finishReason]
	if stopReason == "" {
		stopReason = "end_turn"
	}

	id, _ := openaiResp["id"].(string)

	return &models.AnthropicMessageResponse{
		ID:         id,
		Type:       models.MessageTypeMessage,
		Role:       models.RoleAssistant,
		Content:    contentBlocks,
		Model:      model,
		StopReason: &stopReason,
		Usage:      usage,
	}, nil
}

func extractContentBlocksWithReasoning(choice map[string]interface{}) []models.AnthropicContentBlock {
	messageData, _ := choice["message"].(map[string]interface{})
	if messageData == nil {
		return []models.AnthropicContentBlock{{Type: models.ContentTypeText, Text: strPtr("")}}
	}

	var blocks []models.AnthropicContentBlock

	// Process reasoning content as thinking block
	reasoningContent, _ := messageData["reasoning_content"].(string)
	if strings.TrimSpace(reasoningContent) != "" {
		sig := fmt.Sprintf("%d", time.Now().UnixMilli())
		blocks = append(blocks, models.AnthropicContentBlock{
			Type:      models.ContentTypeThinking,
			Thinking:  &reasoningContent,
			Signature: &sig,
		})
	}

	// Process regular content
	contentStr, _ := messageData["content"].(string)
	if strings.TrimSpace(contentStr) != "" {
		// Check for thinking tags in content
		thinkPattern := regexp.MustCompile(`(?s)\s*thinking(.*?)\s*response`)
		thinkMatches := thinkPattern.FindStringSubmatch(contentStr)

		if len(thinkMatches) > 1 && !hasThinkingBlock(blocks) {
			thinkingContent := strings.TrimSpace(thinkMatches[1])
			if thinkingContent != "" {
				sig := fmt.Sprintf("%d", time.Now().UnixMilli())
				blocks = append(blocks, models.AnthropicContentBlock{
					Type:      models.ContentTypeThinking,
					Thinking:  &thinkingContent,
					Signature: &sig,
				})
			}
		}

		// Remove thinking tags, keep regular content
		cleanContent := thinkPattern.ReplaceAllString(contentStr, "")
		cleanContent = strings.TrimSpace(cleanContent)
		if cleanContent != "" {
			blocks = append(blocks, models.AnthropicContentBlock{
				Type: models.ContentTypeText,
				Text: &cleanContent,
			})
		}
	}

	// Process tool calls
	if toolCallsRaw, ok := messageData["tool_calls"].([]interface{}); ok {
		for _, tcRaw := range toolCallsRaw {
			tc, _ := tcRaw.(map[string]interface{})
			id, _ := tc["id"].(string)
			function, _ := tc["function"].(map[string]interface{})
			name, _ := function["name"].(string)
			arguments, _ := function["arguments"].(string)

			input := safeJSONParse(arguments)

			blocks = append(blocks, models.AnthropicContentBlock{
				Type:  models.ContentTypeToolUse,
				ID:    &id,
				Name:  &name,
				Input: input,
			})
		}
	}

	if len(blocks) == 0 {
		blocks = []models.AnthropicContentBlock{{Type: models.ContentTypeText, Text: strPtr("")}}
	}

	return blocks
}

func hasThinkingBlock(blocks []models.AnthropicContentBlock) bool {
	for _, b := range blocks {
		if b.Type == models.ContentTypeThinking {
			return true
		}
	}
	return false
}

func convertUsage(openaiResp map[string]interface{}, requestID string, contentBlocks []models.AnthropicContentBlock) models.AnthropicUsage {
	usageData, _ := openaiResp["usage"].(map[string]interface{})

	promptTokens := 0
	completionTokens := 0

	if usageData != nil {
		if pt, ok := usageData["prompt_tokens"].(float64); ok {
			promptTokens = int(pt)
		}
		if ct, ok := usageData["completion_tokens"].(float64); ok {
			completionTokens = int(ct)
		}
	}

	// Fallback to cached tokens
	if promptTokens == 0 && requestID != "" {
		if cached, ok := common.GetCachedTokens(requestID, false); ok {
			promptTokens = cached
		}
	}

	// Fallback completion tokens
	if completionTokens == 0 && len(contentBlocks) > 0 {
		completionTokens, _ = common.GlobalCounter.CountResponseTokens(contentBlocks)
	}

	usage := models.AnthropicUsage{
		InputTokens:  promptTokens,
		OutputTokens: &completionTokens,
	}

	return usage
}

func strPtr(s string) *string {
	return &s
}
