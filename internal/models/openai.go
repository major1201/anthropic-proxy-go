// Package models provides OpenAI API data types.
package models

import "encoding/json"

// OpenAIMessageContent is a content item within a message.
type OpenAIMessageContent struct {
	Type     string           `json:"type"`
	Text     *string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL  `json:"image_url,omitempty"`
}

// OpenAIImageURL is an image URL configuration.
type OpenAIImageURL struct {
	URL    string  `json:"url"`
	Detail *string `json:"detail,omitempty"`
}

// OpenAIMessage is a single message in a conversation.
type OpenAIMessage struct {
	Role            string           `json:"role"`
	Content         json.RawMessage  `json:"content,omitempty"` // string, []OpenAIMessageContent, or nil
	Name            *string          `json:"name,omitempty"`
	ToolCalls       json.RawMessage  `json:"tool_calls,omitempty"`
	ToolCallID      *string          `json:"tool_call_id,omitempty"`
	Refusal         *string          `json:"refusal,omitempty"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
}

// OpenAIToolCallFunction is a tool call function.
type OpenAIToolCallFunction struct {
	Name      *string `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}

// OpenAIToolCall is a tool call.
type OpenAIToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function OpenAIToolCallFunction `json:"function"`
}

// OpenAIDeltaToolCall is a tool call delta in streaming.
type OpenAIDeltaToolCall struct {
	Index    *int                   `json:"index,omitempty"`
	ID       *string                `json:"id,omitempty"`
	Type     *string                `json:"type,omitempty"`
	Function *OpenAIToolCallFunction `json:"function,omitempty"`
}

// OpenAIToolFunction is a tool function definition.
type OpenAIToolFunction struct {
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// OpenAITool is a tool definition.
type OpenAITool struct {
	Type     string            `json:"type"`
	Function OpenAIToolFunction `json:"function"`
}

// OpenAIRequest is the top-level OpenAI API request.
type OpenAIRequest struct {
	Model               string          `json:"model"`
	Messages            []OpenAIMessage `json:"messages"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	TopK                *int            `json:"top_k,omitempty"`
	Stream              *bool           `json:"stream,omitempty"`
	StreamOptions       json.RawMessage `json:"stream_options,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"` // string or []string
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	Logprobs            *bool           `json:"logprobs,omitempty"`
	TopLogprobs         *int            `json:"top_logprobs,omitempty"`
	LogitBias           json.RawMessage `json:"logit_bias,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Seed                *int            `json:"seed,omitempty"`
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
	Tools               []OpenAITool    `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	User                *string         `json:"user,omitempty"`
	Think               *bool           `json:"think,omitempty"`
}

// OpenAIChoiceDelta is the delta in a streaming choice.
type OpenAIChoiceDelta struct {
	Role             *string                `json:"role,omitempty"`
	Content          *string                `json:"content,omitempty"`
	ReasoningContent *string                `json:"reasoning_content,omitempty"`
	ToolCalls        json.RawMessage        `json:"tool_calls,omitempty"`
}

// OpenAIChoice is a response choice.
type OpenAIChoice struct {
	Index        int                `json:"index"`
	Message      *OpenAIMessage     `json:"message,omitempty"`
	Delta        *OpenAIChoiceDelta `json:"delta,omitempty"`
	FinishReason *string            `json:"finish_reason,omitempty"`
}

// OpenAIUsage is usage statistics.
type OpenAIUsage struct {
	PromptTokens            int             `json:"prompt_tokens"`
	CompletionTokens        int             `json:"completion_tokens"`
	TotalTokens             int             `json:"total_tokens"`
	CompletionTokensDetails json.RawMessage `json:"completion_tokens_details,omitempty"`
	PromptTokensDetails     json.RawMessage `json:"prompt_tokens_details,omitempty"`
}

// OpenAIResponse is the non-streaming response.
type OpenAIResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             OpenAIUsage    `json:"usage"`
	SystemFingerprint *string        `json:"system_fingerprint,omitempty"`
}

// OpenAIStreamResponse is a streaming response chunk.
type OpenAIStreamResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             *OpenAIUsage   `json:"usage,omitempty"`
	SystemFingerprint *string        `json:"system_fingerprint,omitempty"`
}

// OpenAIErrorDetail is error detail.
type OpenAIErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param,omitempty"`
	Code    *string `json:"code,omitempty"`
}

// OpenAIErrorResponse is the error response.
type OpenAIErrorResponse struct {
	Error OpenAIErrorDetail `json:"error"`
}
