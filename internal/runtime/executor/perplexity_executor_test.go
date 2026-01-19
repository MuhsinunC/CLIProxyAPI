package executor

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestPerplexityExecutor_Execute(t *testing.T) {
	// Skip if no session token provided
	sessionToken := os.Getenv("PERPLEXITY_SESSION_TOKEN")
	if sessionToken == "" {
		t.Skip("PERPLEXITY_SESSION_TOKEN not set")
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

	// Create an OpenAI-format request
	payload := map[string]interface{}{
		"model": "claude-4.5-sonnet",
		"messages": []map[string]string{
			{"role": "user", "content": "What is 2+2? Answer in one word."},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	req := cliproxyexecutor.Request{
		Model:   "claude-4.5-sonnet",
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

	if len(resp.Payload) == 0 {
		t.Fatal("Response payload is empty")
	}

	t.Logf("Response: %s", string(resp.Payload))
}

func TestPerplexityExecutor_WhatModel(t *testing.T) {
	// Skip if no session token provided
	sessionToken := os.Getenv("PERPLEXITY_SESSION_TOKEN")
	if sessionToken == "" {
		t.Skip("PERPLEXITY_SESSION_TOKEN not set")
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

	payload := map[string]interface{}{
		"model": "claude-4.5-sonnet",
		"messages": []map[string]string{
			{"role": "user", "content": "What model are you?"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	req := cliproxyexecutor.Request{
		Model:   "claude-4.5-sonnet",
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

	// Parse and display the response content
	var result map[string]interface{}
	json.Unmarshal(resp.Payload, &result)

	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				t.Logf("\n=== Claude 4.5 Sonnet Response ===\n%s\n", msg["content"])
			}
		}
	}
}

func TestPerplexityExecutor_Execute_GPT52(t *testing.T) {
	// Skip if no session token provided
	sessionToken := os.Getenv("PERPLEXITY_SESSION_TOKEN")
	if sessionToken == "" {
		t.Skip("PERPLEXITY_SESSION_TOKEN not set")
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

	// Create an OpenAI-format request for GPT-5.2
	payload := map[string]interface{}{
		"model": "gpt-5.2",
		"messages": []map[string]string{
			{"role": "user", "content": "What is 3+3? Answer in one word."},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	req := cliproxyexecutor.Request{
		Model:   "gpt-5.2",
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

	if len(resp.Payload) == 0 {
		t.Fatal("Response payload is empty")
	}

	t.Logf("Response: %s", string(resp.Payload))
}

func TestPerplexityExecutor_Execute_ClaudeOpus(t *testing.T) {
	// Skip if no session token provided
	sessionToken := os.Getenv("PERPLEXITY_SESSION_TOKEN")
	if sessionToken == "" {
		t.Skip("PERPLEXITY_SESSION_TOKEN not set")
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

	// Create an OpenAI-format request for Claude Opus 4.5
	payload := map[string]interface{}{
		"model": "claude-4.5-opus",
		"messages": []map[string]string{
			{"role": "user", "content": "What is 5+5? Answer in one word."},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	req := cliproxyexecutor.Request{
		Model:   "claude-4.5-opus",
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

	if len(resp.Payload) == 0 {
		t.Fatal("Response payload is empty")
	}

	// Parse and display the response content
	var result map[string]interface{}
	json.Unmarshal(resp.Payload, &result)

	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				t.Logf("\n=== Claude Opus 4.5 Response ===\n%s\n", msg["content"])
			}
		}
	}
}
