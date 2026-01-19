package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestPerplexityExecutor_AllModels_WhatModel(t *testing.T) {
	sessionToken := os.Getenv("PERPLEXITY_SESSION_TOKEN")
	if sessionToken == "" {
		t.Skip("PERPLEXITY_SESSION_TOKEN not set")
	}

	models := []string{
		"claude-4.5-sonnet",
		"claude-4.5-opus",
		"gpt-5.2",
		"gemini-3-pro",
		"grok-4.1",
	}

	cfg := &config.Config{}
	exec := NewPerplexityExecutor(cfg)

	auth := &cliproxyauth.Auth{
		ID:       "test",
		Provider: "perplexity",
		Metadata: map[string]any{
			"session_token": sessionToken,
		},
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			payload := map[string]interface{}{
				"model": model,
				"messages": []map[string]string{
					{"role": "user", "content": "What model are you? Be specific about your model name and version."},
				},
			}
			payloadBytes, _ := json.Marshal(payload)

			req := cliproxyexecutor.Request{
				Model:   model,
				Payload: payloadBytes,
			}

			opts := cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("openai"),
			}

			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			resp, err := exec.Execute(ctx, auth, req, opts)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			var result map[string]interface{}
			json.Unmarshal(resp.Payload, &result)

			if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if msg, ok := choice["message"].(map[string]interface{}); ok {
						content := msg["content"].(string)
						// Truncate for readability
						if len(content) > 500 {
							content = content[:500] + "..."
						}
						fmt.Printf("\n=== %s ===\n%s\n", model, content)
					}
				}
			}
		})
		// Small delay between requests to avoid rate limiting
		time.Sleep(2 * time.Second)
	}
}
