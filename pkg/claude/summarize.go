// Package claude provides an Anthropic API client for enriching transaction
// memos using Claude's language model.
package claude

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const model = anthropic.ModelClaudeHaiku4_5_20251001

// Summarizer calls the Anthropic API to condense transaction descriptions.
type Summarizer struct {
	client *anthropic.Client
}

// New creates a Summarizer using the given API key.
func New(apiKey string) *Summarizer {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Summarizer{client: &c}
}

// SummarizeMemo condenses a list of item descriptions into short labels
// (3–4 words each), returned as a single comma-separated string.
//
// Single item: "Qunol Mega CoQ10 Softgels 200mg 3X Better Absorption 120ct" → "CoQ10 softgels 120ct"
// Multiple:    ["USB-C cable 6ft", "AAA batteries 24pk"] → "USB-C cable, AAA batteries"
func (s *Summarizer) SummarizeMemo(ctx context.Context, items []string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	var prompt string
	if len(items) == 1 {
		prompt = fmt.Sprintf(
			"Summarize this product or service name to 3-4 words that capture what it is. "+
				"Return only the short label, nothing else.\n\n%s",
			items[0],
		)
	} else {
		list := make([]string, len(items))
		for i, item := range items {
			list[i] = fmt.Sprintf("%d. %s", i+1, item)
		}
		prompt = fmt.Sprintf(
			"Summarize each of the following items to 3-4 words that capture what it is. "+
				"Return only the labels separated by commas, one per item, in the same order. "+
				"No numbering, no extra text.\n\n%s",
			strings.Join(list, "\n"),
		)
	}

	msg, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 100,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude API: %w", err)
	}

	if len(msg.Content) == 0 {
		return "", fmt.Errorf("claude returned empty response")
	}

	result := strings.TrimSpace(msg.Content[0].Text)
	return result, nil
}
