// Package models provides Anthropic API data types.
package models

import "encoding/json"

// Stream event type constants
const (
	EventMessageStart      = "message_start"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
	EventPing              = "ping"
)

// Content type constants
const (
	ContentTypeText           = "text"
	ContentTypeImage          = "image"
	ContentTypeToolUse        = "tool_use"
	ContentTypeToolResult     = "tool_result"
	ContentTypeThinking       = "thinking"
	ContentTypeTextDelta      = "text_delta"
	ContentTypeInputJSONDelta = "input_json_delta"
	ContentTypeThinkingDelta  = "thinking_delta"
	ContentTypeSignatureDelta = "signature_delta"
)

// Message type constants
const (
	MessageTypeMessage = "message"
	MessageTypeError   = "error"
)

// Role constants
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// AnthropicMessageContent is a content item within a message.
type AnthropicMessageContent struct {
	Type       string          `json:"type"`
	Text       *string         `json:"text,omitempty"`
	Source     json.RawMessage `json:"source,omitempty"`
	ID         *string         `json:"id,omitempty"`
	Name       *string         `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ToolUseID  *string         `json:"tool_use_id,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	IsError    *bool           `json:"is_error,omitempty"`
}

// AnthropicMessage is a single message in a conversation.
type AnthropicMessage struct {
	Role    string                     `json:"role"`
	Content json.RawMessage            `json:"content"` // string or []AnthropicMessageContent
}

// AnthropicSystemMessage is a system message.
type AnthropicSystemMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// AnthropicToolDefinition is a tool definition.
type AnthropicToolDefinition struct {
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Type        *string         `json:"type,omitempty"`
	MaxUses     *int            `json:"max_uses,omitempty"`
}

// AnthropicRequest is the top-level Anthropic API request.
type AnthropicRequest struct {
	Model         string                     `json:"model"`
	Messages      []AnthropicMessage         `json:"messages"`
	MaxTokens     int                        `json:"max_tokens"`
	System        json.RawMessage            `json:"system,omitempty"`        // string or []AnthropicSystemMessage
	Tools         []AnthropicToolDefinition  `json:"tools,omitempty"`
	ToolChoice    json.RawMessage            `json:"tool_choice,omitempty"`   // string or object
	Metadata      map[string]interface{}     `json:"metadata,omitempty"`
	StopSequences []string                   `json:"stop_sequences,omitempty"`
	Stream        *bool                      `json:"stream,omitempty"`
	Temperature   *float64                   `json:"temperature,omitempty"`
	TopP          *float64                   `json:"top_p,omitempty"`
	TopK          *int                       `json:"top_k,omitempty"`
	Thinking      json.RawMessage            `json:"thinking,omitempty"`      // bool or object
}

// AnthropicContentBlock is a content block in a response.
type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      *string         `json:"text,omitempty"`
	ID        *string         `json:"id,omitempty"`
	Name      *string         `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature *string         `json:"signature,omitempty"`
}

// AnthropicUsage is usage statistics.
type AnthropicUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             *int    `json:"output_tokens,omitempty"`
	CacheCreationInputTokens *int    `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int    `json:"cache_read_input_tokens,omitempty"`
	ServiceTier              *string `json:"service_tier,omitempty"`
}

// AnthropicMessageResponse is the non-streaming response.
type AnthropicMessageResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   *string                 `json:"stop_reason,omitempty"`
	StopSequence *string                 `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicErrorDetail is error detail.
type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AnthropicErrorResponse is the error response.
type AnthropicErrorResponse struct {
	Type  string               `json:"type"`
	Error AnthropicErrorDetail `json:"error"`
}

// Stream message types

// MessageDelta is the delta in message_delta event.
type MessageDelta struct {
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// AnthropicStreamMessageStartMessage is the message payload in message_start.
type AnthropicStreamMessageStartMessage struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Model        string          `json:"model"`
	Content      json.RawMessage `json:"content"`
	StopReason   *string         `json:"stop_reason,omitempty"`
	StopSequence *string         `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage  `json:"usage"`
}

// AnthropicStreamMessage is a stream message event (message_start, message_delta, message_stop).
type AnthropicStreamMessage struct {
	Type    string                             `json:"type"`
	Message *AnthropicStreamMessageStartMessage `json:"message,omitempty"`
	Delta   *MessageDelta                      `json:"delta,omitempty"`
	Usage   *AnthropicUsage                    `json:"usage,omitempty"`
}

// Delta is a content delta.
type Delta struct {
	Type        string  `json:"type,omitempty"`
	Text        *string `json:"text,omitempty"`
	Thinking    *string `json:"thinking,omitempty"`
	Signature   *string `json:"signature,omitempty"`
	PartialJSON *string `json:"partial_json,omitempty"`
}

// ContentBlock is a content block in stream events.
type ContentBlock struct {
	Type      string          `json:"type,omitempty"`
	Text      *string         `json:"text,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature *string         `json:"signature,omitempty"`
	ID        *string         `json:"id,omitempty"`
	Name      *string         `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

// AnthropicStreamContentBlockStart is content_block_start event.
type AnthropicStreamContentBlockStart struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// AnthropicStreamContentBlock is content_block_delta event.
type AnthropicStreamContentBlock struct {
	Type         string        `json:"type"`
	Index        int           `json:"index"`
	Delta        *Delta        `json:"delta,omitempty"`
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
	Usage        *Delta        `json:"usage,omitempty"`
}

// AnthropicStreamContentBlockStop is content_block_stop event.
type AnthropicStreamContentBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// AnthropicPing is a ping event.
type AnthropicPing struct {
	Type string `json:"type"`
}
