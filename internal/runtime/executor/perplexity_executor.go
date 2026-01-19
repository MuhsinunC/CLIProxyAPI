package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	perplexityauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/perplexity"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	utls "github.com/refraction-networking/utls"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	perplexityEndpoint   = "https://www.perplexity.ai/rest/sse/perplexity_ask"
	perplexityAPIVersion = "2.18"
)

// PerplexityExecutor executes chat completions against the Perplexity AI web API.
type PerplexityExecutor struct {
	cfg *config.Config
}

// NewPerplexityExecutor constructs a new executor instance.
func NewPerplexityExecutor(cfg *config.Config) *PerplexityExecutor {
	return &PerplexityExecutor{cfg: cfg}
}

// Identifier returns the provider key.
func (e *PerplexityExecutor) Identifier() string { return "perplexity" }

// perplexityModelMapping maps user-friendly model names to Perplexity internal codes.
var perplexityModelMapping = map[string]struct {
	mode            string
	modelPreference string
}{
	// Default Perplexity Pro / Best
	"pplx-pro":       {mode: "copilot", modelPreference: "pplx_pro"},
	"perplexity-pro": {mode: "copilot", modelPreference: "pplx_pro"},
	"best":           {mode: "copilot", modelPreference: "pplx_pro"},

	// Claude Sonnet 4.5 (with/without thinking)
	"claude-4.5-sonnet":          {mode: "copilot", modelPreference: "claude45sonnet"},
	"claude-sonnet-4.5":          {mode: "copilot", modelPreference: "claude45sonnet"},
	"claude45sonnet":             {mode: "copilot", modelPreference: "claude45sonnet"},
	"claude-4.5-sonnet-thinking": {mode: "copilot", modelPreference: "claude45sonnetthinking"},
	"claude45sonnetthinking":     {mode: "copilot", modelPreference: "claude45sonnetthinking"},

	// Claude Opus 4.5 (max tier)
	"claude-4.5-opus":          {mode: "copilot", modelPreference: "claude45opus"},
	"claude-opus-4.5":          {mode: "copilot", modelPreference: "claude45opus"},
	"claude45opus":             {mode: "copilot", modelPreference: "claude45opus"},
	"claude-4.5-opus-thinking": {mode: "copilot", modelPreference: "claude45opusthinking"},
	"claude45opusthinking":     {mode: "copilot", modelPreference: "claude45opusthinking"},

	// GPT-5.2 (with/without thinking/reasoning)
	"gpt-5.2":          {mode: "copilot", modelPreference: "gpt52"},
	"gpt52":            {mode: "copilot", modelPreference: "gpt52"},
	"gpt-5.2-thinking": {mode: "copilot", modelPreference: "gpt52_thinking"},
	"gpt-5.2-reasoning": {mode: "copilot", modelPreference: "gpt52_thinking"},
	"gpt52_thinking":   {mode: "copilot", modelPreference: "gpt52_thinking"},

	// Gemini 3 Flash (with/without reasoning)
	"gemini-3-flash":           {mode: "copilot", modelPreference: "gemini3flash"},
	"gemini3flash":             {mode: "copilot", modelPreference: "gemini3flash"},
	"gemini-3-flash-thinking":  {mode: "copilot", modelPreference: "gemini3flashthinking"},
	"gemini-3-flash-reasoning": {mode: "copilot", modelPreference: "gemini3flashthinking"},
	"gemini3flashthinking":     {mode: "copilot", modelPreference: "gemini3flashthinking"},

	// Gemini 3 Pro
	"gemini-3-pro":   {mode: "copilot", modelPreference: "gemini3pro"},
	"gemini-3.0-pro": {mode: "copilot", modelPreference: "gemini3pro"},
	"gemini3pro":     {mode: "copilot", modelPreference: "gemini3pro"},
	"gemini30pro":    {mode: "copilot", modelPreference: "gemini3pro"},

	// Grok 4.1 (with/without reasoning)
	"grok-4.1":           {mode: "copilot", modelPreference: "grok41nonreasoning"},
	"grok4.1":            {mode: "copilot", modelPreference: "grok41nonreasoning"},
	"grok41nonreasoning": {mode: "copilot", modelPreference: "grok41nonreasoning"},
	"grok-4.1-reasoning": {mode: "copilot", modelPreference: "grok41reasoning"},
	"grok41reasoning":    {mode: "copilot", modelPreference: "grok41reasoning"},

	// Kimi K2 Thinking
	"kimi-k2":          {mode: "copilot", modelPreference: "kimik2thinking"},
	"kimi-k2-thinking": {mode: "copilot", modelPreference: "kimik2thinking"},
	"kimik2thinking":   {mode: "copilot", modelPreference: "kimik2thinking"},

	// Sonar (experimental)
	"sonar":        {mode: "copilot", modelPreference: "experimental"},
	"experimental": {mode: "copilot", modelPreference: "experimental"},

	// Auto/concise mode (default/turbo)
	"auto":  {mode: "concise", modelPreference: "turbo"},
	"turbo": {mode: "concise", modelPreference: "turbo"},

	// Deep research
	"deep-research": {mode: "copilot", modelPreference: "pplx_alpha"},
	"pplx_alpha":    {mode: "copilot", modelPreference: "pplx_alpha"},

	// Reasoning mode defaults
	"pplx-reasoning":       {mode: "copilot", modelPreference: "pplx_reasoning"},
	"perplexity-reasoning": {mode: "copilot", modelPreference: "pplx_reasoning"},
}

// perplexityRequest is the request format for Perplexity's internal API.
type perplexityRequest struct {
	QueryStr string                 `json:"query_str"`
	Params   perplexityRequestParams `json:"params"`
}

type perplexityRequestParams struct {
	Attachments         []string `json:"attachments"`
	FrontendContextUUID string   `json:"frontend_context_uuid"`
	FrontendUUID        string   `json:"frontend_uuid"`
	IsIncognito         bool     `json:"is_incognito"`
	Language            string   `json:"language"`
	LastBackendUUID     *string  `json:"last_backend_uuid"`
	Mode                string   `json:"mode"`
	ModelPreference     string   `json:"model_preference"`
	Source              string   `json:"source"`
	Sources             []string `json:"sources"`
	Version             string   `json:"version"`
}

// PrepareRequest injects Perplexity credentials into the outgoing HTTP request.
func (e *PerplexityExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	sessionToken := perplexityCreds(auth)
	if sessionToken != "" {
		req.Header.Set("Cookie", perplexityauth.SessionTokenCookieName+"="+sessionToken)
	}
	return nil
}

// HttpRequest injects credentials and executes the request with TLS fingerprinting.
func (e *PerplexityExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("perplexity executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := newPerplexityHTTPClient()
	return httpClient.Do(httpReq)
}

// Execute performs a non-streaming chat completion request.
func (e *PerplexityExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	sessionToken := perplexityCreds(auth)
	if sessionToken == "" {
		err = fmt.Errorf("perplexity executor: missing session token")
		return resp, err
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	// Convert OpenAI format to Perplexity format
	perplexityReq, err := e.convertToPerplexityRequest(req)
	if err != nil {
		return resp, err
	}

	body, err := json.Marshal(perplexityReq)
	if err != nil {
		return resp, fmt.Errorf("perplexity executor: marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, perplexityEndpoint, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}
	applyPerplexityHeaders(httpReq, sessionToken)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       perplexityEndpoint,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newPerplexityHTTPClient()
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("perplexity executor: close response body error: %v", errClose)
		}
	}()
	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		log.Debugf("perplexity request error: status %d body %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), b))
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return resp, err
	}

	// Read SSE response and extract answer
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	appendAPIResponseChunk(ctx, e.cfg, data)

	// Parse SSE and extract final answer
	answer, model := parsePerplexitySSEResponse(string(data))

	// Convert to OpenAI format
	from := sdktranslator.FromString("perplexity")
	to := opts.SourceFormat
	openAIResp := buildOpenAIResponse(answer, model, req.Model)

	out := sdktranslator.TranslateNonStream(ctx, from, to, req.Model, bytes.Clone(opts.OriginalRequest), body, openAIResp, nil)
	resp = cliproxyexecutor.Response{Payload: []byte(out)}
	reporter.ensurePublished(ctx)
	return resp, nil
}

// ExecuteStream performs a streaming chat completion request.
func (e *PerplexityExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (stream <-chan cliproxyexecutor.StreamChunk, err error) {
	sessionToken := perplexityCreds(auth)
	if sessionToken == "" {
		err = fmt.Errorf("perplexity executor: missing session token")
		return nil, err
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	// Convert OpenAI format to Perplexity format
	perplexityReq, err := e.convertToPerplexityRequest(req)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(perplexityReq)
	if err != nil {
		return nil, fmt.Errorf("perplexity executor: marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, perplexityEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	applyPerplexityHeaders(httpReq, sessionToken)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       perplexityEndpoint,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newPerplexityHTTPClient()
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, _ := io.ReadAll(httpResp.Body)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("perplexity executor: close response body error: %v", errClose)
		}
		appendAPIResponseChunk(ctx, e.cfg, data)
		log.Debugf("perplexity streaming error: status %d body %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		err = statusErr{code: httpResp.StatusCode, msg: string(data)}
		return nil, err
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	stream = out
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("perplexity executor: close response body error: %v", errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800) // 50MB
		var lastContent string
		var displayModel string

		for scanner.Scan() {
			line := scanner.Text()
			appendAPIResponseChunk(ctx, e.cfg, []byte(line))

			// Parse SSE event
			if strings.HasPrefix(line, "data:") {
				dataStr := strings.TrimPrefix(line, "data:")
				dataStr = strings.TrimSpace(dataStr)

				var eventData map[string]interface{}
				if err := json.Unmarshal([]byte(dataStr), &eventData); err != nil {
					continue
				}

				if model, ok := eventData["display_model"].(string); ok && model != "" {
					displayModel = model
				}

				// Extract content from blocks
				newContent := extractContentFromBlocks(eventData)
				if newContent != "" && newContent != lastContent {
					// Send delta (new content since last)
					delta := newContent
					if len(newContent) > len(lastContent) && strings.HasPrefix(newContent, lastContent) {
						delta = newContent[len(lastContent):]
					}
					lastContent = newContent

					// Convert to OpenAI streaming format
					chunk := buildOpenAIStreamChunk(delta, displayModel, req.Model, false)
					out <- cliproxyexecutor.StreamChunk{Payload: chunk}
				}

				// Check for end of stream
				if final, ok := eventData["final_sse_message"].(bool); ok && final {
					// Send final chunk
					chunk := buildOpenAIStreamChunk("", displayModel, req.Model, true)
					out <- cliproxyexecutor.StreamChunk{Payload: chunk}
					reporter.ensurePublished(ctx)
					return
				}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}
		reporter.ensurePublished(ctx)
	}()

	return stream, nil
}

// CountTokens estimates token count.
func (e *PerplexityExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), false)

	enc, err := tokenizerForModel(req.Model)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("perplexity executor: tokenizer init failed: %w", err)
	}

	count, err := countOpenAIChatTokens(enc, body)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("perplexity executor: token counting failed: %w", err)
	}

	usageJSON := buildOpenAIUsageJSON(count)
	translated := sdktranslator.TranslateTokenCount(ctx, to, from, count, usageJSON)
	return cliproxyexecutor.Response{Payload: []byte(translated)}, nil
}

// Refresh is a no-op for Perplexity (session tokens don't need refresh).
func (e *PerplexityExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	// Perplexity session tokens are long-lived, no refresh needed
	return auth, nil
}

// convertToPerplexityRequest converts an OpenAI-format request to Perplexity format.
func (e *PerplexityExecutor) convertToPerplexityRequest(req cliproxyexecutor.Request) (*perplexityRequest, error) {
	// Extract messages from payload
	messages := gjson.GetBytes(req.Payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return nil, fmt.Errorf("perplexity executor: messages field required")
	}

	// Build query string from messages (concatenate user messages)
	var queryParts []string
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		content := msg.Get("content").String()
		if role == "user" && content != "" {
			queryParts = append(queryParts, content)
		}
		return true
	})

	if len(queryParts) == 0 {
		return nil, fmt.Errorf("perplexity executor: no user messages found")
	}

	// Get last user message as the query
	queryStr := queryParts[len(queryParts)-1]

	// Map model to Perplexity mode and preference
	modelKey := strings.ToLower(req.Model)
	mapping, ok := perplexityModelMapping[modelKey]
	if !ok {
		// Default to claude45sonnet if unknown
		mapping = perplexityModelMapping["claude-4.5-sonnet"]
	}

	return &perplexityRequest{
		QueryStr: queryStr,
		Params: perplexityRequestParams{
			Attachments:         []string{},
			FrontendContextUUID: uuid.New().String(),
			FrontendUUID:        uuid.New().String(),
			IsIncognito:         false,
			Language:            "en-US",
			LastBackendUUID:     nil,
			Mode:                mapping.mode,
			ModelPreference:     mapping.modelPreference,
			Source:              "default",
			Sources:             []string{}, // Empty for direct model access (no search wrapper)
			Version:             perplexityAPIVersion,
		},
	}, nil
}

// applyPerplexityHeaders sets required headers for Perplexity requests.
// These headers must closely match what a real Chrome browser would send.
func applyPerplexityHeaders(r *http.Request, sessionToken string) {
	// Core request headers
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")

	// Chrome-like headers
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	r.Header.Set("Origin", "https://www.perplexity.ai")
	r.Header.Set("Referer", "https://www.perplexity.ai/")

	// Security headers that Chrome sends
	r.Header.Set("Sec-Ch-Ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
	r.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	r.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)
	r.Header.Set("Sec-Fetch-Dest", "empty")
	r.Header.Set("Sec-Fetch-Mode", "cors")
	r.Header.Set("Sec-Fetch-Site", "same-origin")

	// Cookie with session token
	r.Header.Set("Cookie", perplexityauth.SessionTokenCookieName+"="+sessionToken)
}

// perplexityCreds extracts session token from auth.
func perplexityCreds(a *cliproxyauth.Auth) string {
	if a == nil {
		return ""
	}
	if a.Metadata != nil {
		if v, ok := a.Metadata["session_token"].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	if a.Attributes != nil {
		if v := strings.TrimSpace(a.Attributes["session_token"]); v != "" {
			return v
		}
	}
	return ""
}

// newPerplexityHTTPClient creates an HTTP client with Chrome TLS fingerprinting.
// Uses a custom ClientHello based on Chrome 120 but with HTTP/1.1 only ALPN
// to avoid HTTP/2 protocol negotiation issues with Go's http.Transport.
func newPerplexityHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Create TCP connection
				dialer := &net.Dialer{Timeout: 30 * time.Second}
				conn, err := dialer.DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}

				// Get hostname for SNI
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					host = addr
				}

				// Create uTLS connection with custom fingerprint
				tlsConn := utls.UClient(conn, &utls.Config{
					ServerName:         host,
					InsecureSkipVerify: false,
				}, utls.HelloCustom)

				// Get Chrome 120 spec and modify ALPN to force HTTP/1.1
				spec, err := utls.UTLSIdToSpec(utls.HelloChrome_120)
				if err != nil {
					conn.Close()
					return nil, fmt.Errorf("failed to get Chrome spec: %w", err)
				}

				// Find and replace ALPN extension to use only HTTP/1.1
				for i, ext := range spec.Extensions {
					if _, ok := ext.(*utls.ALPNExtension); ok {
						spec.Extensions[i] = &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}}
						break
					}
				}

				// Apply the modified spec
				if err := tlsConn.ApplyPreset(&spec); err != nil {
					conn.Close()
					return nil, fmt.Errorf("failed to apply TLS preset: %w", err)
				}

				// Perform handshake
				if err := tlsConn.Handshake(); err != nil {
					conn.Close()
					return nil, err
				}

				return tlsConn, nil
			},
			// Disable HTTP/2 to use HTTP/1.1
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// parsePerplexitySSEResponse parses the SSE response and extracts the final answer.
func parsePerplexitySSEResponse(data string) (answer, model string) {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimPrefix(line, "data:")
			dataStr = strings.TrimSpace(dataStr)

			var eventData map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &eventData); err != nil {
				continue
			}

			if m, ok := eventData["display_model"].(string); ok && m != "" {
				model = m
			}

			if final, ok := eventData["final_sse_message"].(bool); ok && final {
				answer = extractContentFromBlocks(eventData)
				break
			}
		}
	}
	return answer, model
}

// extractContentFromBlocks extracts text content from Perplexity response blocks.
func extractContentFromBlocks(data map[string]interface{}) string {
	blocks, ok := data["blocks"].([]interface{})
	if !ok {
		return ""
	}

	var content strings.Builder
	for _, block := range blocks {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		usage := blockMap["intended_usage"]
		if usage == "ask_text" {
			if mdBlock, ok := blockMap["markdown_block"].(map[string]interface{}); ok {
				if chunks, ok := mdBlock["chunks"].([]interface{}); ok {
					for _, chunk := range chunks {
						if s, ok := chunk.(string); ok {
							content.WriteString(s)
						}
					}
				}
			}
		}
	}

	return content.String()
}

// buildOpenAIResponse creates an OpenAI-format response from Perplexity data.
func buildOpenAIResponse(content, displayModel, requestedModel string) []byte {
	resp := map[string]interface{}{
		"id":      "chatcmpl-" + uuid.New().String()[:8],
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   requestedModel,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}

	data, _ := json.Marshal(resp)
	return data
}

// buildOpenAIStreamChunk creates an OpenAI streaming chunk.
func buildOpenAIStreamChunk(delta, displayModel, requestedModel string, done bool) []byte {
	if done {
		return []byte("data: [DONE]\n\n")
	}

	chunk := map[string]interface{}{
		"id":      "chatcmpl-" + uuid.New().String()[:8],
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   requestedModel,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]string{
					"content": delta,
				},
				"finish_reason": nil,
			},
		},
	}

	data, _ := json.Marshal(chunk)
	return append([]byte("data: "), append(data, []byte("\n\n")...)...)
}
