// Package common provides token counting using tiktoken.
package common

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/major1201/anthropic-proxy-go/internal/models"
	"github.com/pkoukk/tiktoken-go"
)

// TokenCounter counts tokens using the o200k_base encoding.
type TokenCounter struct {
	once    sync.Once
	encoder *tiktoken.Tiktoken
	initErr error
}

// Global instance for convenience.
var GlobalCounter = &TokenCounter{}

func (tc *TokenCounter) getEncoder() (*tiktoken.Tiktoken, error) {
	tc.once.Do(func() {
		tc.encoder, tc.initErr = tiktoken.GetEncoding("o200k_base")
	})
	return tc.encoder, tc.initErr
}

// CountTokens calculates the total token count for a complete request.
func (tc *TokenCounter) CountTokens(messages interface{}, system interface{}, tools interface{}) (int, error) {
	encoder, err := tc.getEncoder()
	if err != nil {
		return 0, err
	}

	var textParts []string

	// Process messages
	if messages != nil {
		if msgs, ok := messages.([]interface{}); ok {
			for _, msg := range msgs {
				textParts = append(textParts, extractMessageText(msg)...)
			}
		}
	}

	// Process system prompt
	if system != nil {
		switch s := system.(type) {
		case string:
			textParts = append(textParts, s)
		case []interface{}:
			for _, item := range s {
				if m, ok := item.(map[string]interface{}); ok {
					if t, ok := m["type"].(string); ok && t == "text" {
						if text, ok := m["text"].(string); ok {
							textParts = append(textParts, text)
						}
					}
				}
			}
		}
	}

	// Process tools
	if tools != nil {
		if toolList, ok := tools.([]interface{}); ok {
			for _, tool := range toolList {
				if m, ok := tool.(map[string]interface{}); ok {
					if name, ok := m["name"].(string); ok {
						textParts = append(textParts, name)
					}
					if desc, ok := m["description"].(string); ok {
						textParts = append(textParts, desc)
					}
					if schema, ok := m["input_schema"]; ok {
						if schemaJSON, err := json.Marshal(schema); err == nil {
							textParts = append(textParts, string(schemaJSON))
						}
					}
				}
			}
		}
	}

	combined := strings.Join(textParts, "")
	tokens := encoder.Encode(combined, nil, nil)
	return len(tokens), nil
}

func extractMessageText(msg interface{}) []string {
	var texts []string

	m, ok := msg.(map[string]interface{})
	if !ok {
		return texts
	}

	content := m["content"]
	switch c := content.(type) {
	case string:
		texts = append(texts, c)
	case []interface{}:
		for _, part := range c {
			if p, ok := part.(map[string]interface{}); ok {
				partType, _ := p["type"].(string)
				switch partType {
				case "text":
					if text, ok := p["text"].(string); ok {
						texts = append(texts, text)
					}
				case "tool_use":
					if input, ok := p["input"]; ok {
						if inputJSON, err := json.Marshal(input); err == nil {
							texts = append(texts, string(inputJSON))
						}
					}
				}
			}
		}
	}

	return texts
}

// CountResponseTokens calculates token count for response content blocks.
func (tc *TokenCounter) CountResponseTokens(contentBlocks []models.AnthropicContentBlock) (int, error) {
	encoder, err := tc.getEncoder()
	if err != nil {
		return 0, err
	}

	var textParts []string

	for _, block := range contentBlocks {
		if block.Text != nil {
			textParts = append(textParts, *block.Text)
		}
		if block.Thinking != nil {
			textParts = append(textParts, *block.Thinking)
		}
		if len(block.Input) > 0 {
			textParts = append(textParts, string(block.Input))
		}
		if block.Name != nil {
			textParts = append(textParts, *block.Name)
		}
	}

	combined := strings.Join(textParts, "")
	tokens := encoder.Encode(combined, nil, nil)
	return len(tokens), nil
}
