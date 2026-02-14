package llm

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient wraps the Anthropic SDK.
type AnthropicClient struct {
	client *anthropic.Client
	model  string
}

func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	c := anthropic.NewClient(opts...)
	return &AnthropicClient{
		client: &c,
		model:  model,
	}
}

func (c *AnthropicClient) Chat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	return c.ChatWithHistory(ctx, systemPrompt, []Message{{Role: "user", Content: userMessage}})
}

func (c *AnthropicClient) ChatWithHistory(ctx context.Context, systemPrompt string, messages []Message) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 4096,
		Messages:  toAnthropicMessages(messages),
	}
	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: systemPrompt}}
	}

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	return out.String(), nil
}

func toAnthropicMessages(msgs []Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, len(msgs))
	for i, m := range msgs {
		if m.Role == "assistant" {
			out[i] = anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content))
		} else {
			out[i] = anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content))
		}
	}
	return out
}
