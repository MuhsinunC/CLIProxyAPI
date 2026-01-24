package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

// BudgetTracker monitors API spending for real API tests.
// It tracks both dollar amount and request count to prevent runaway costs.
type BudgetTracker struct {
	mu           sync.Mutex
	budgetUSD    float64
	spentUSD     float64
	requestCount int
	maxRequests  int
	pricing      map[string]ModelPricing
}

// ModelPricing stores the per-million-token pricing for a model.
type ModelPricing struct {
	PromptPerM     float64 // Cost per million prompt tokens
	CompletionPerM float64 // Cost per million completion tokens
}

// NewBudgetTracker creates a budget tracker with the specified budget in USD.
// If budgetStr is empty or invalid, it defaults to $0 (blocking all requests).
func NewBudgetTracker(budgetStr string) *BudgetTracker {
	budget, _ := strconv.ParseFloat(budgetStr, 64)
	bt := &BudgetTracker{
		budgetUSD:   budget,
		maxRequests: 50, // Hard limit to prevent runaway tests
		pricing:     make(map[string]ModelPricing),
	}
	bt.loadPricing()
	return bt
}

// loadPricing fetches pricing from OpenRouter or uses fallback values.
func (bt *BudgetTracker) loadPricing() {
	// Try OpenRouter API first
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey != "" {
		if err := bt.fetchOpenRouterPricing(apiKey); err == nil {
			return
		}
	}

	// Fallback to hardcoded prices (as of late 2024)
	bt.pricing = map[string]ModelPricing{
		// Anthropic models
		"claude-sonnet-4-20250514":   {PromptPerM: 3.0, CompletionPerM: 15.0},
		"claude-3-5-sonnet-20241022": {PromptPerM: 3.0, CompletionPerM: 15.0},
		"claude-3-opus-20240229":     {PromptPerM: 15.0, CompletionPerM: 75.0},
		"claude-3-haiku-20240307":    {PromptPerM: 0.25, CompletionPerM: 1.25},

		// OpenAI models
		"gpt-4o":      {PromptPerM: 2.5, CompletionPerM: 10.0},
		"gpt-4o-mini": {PromptPerM: 0.15, CompletionPerM: 0.6},
		"gpt-4-turbo": {PromptPerM: 10.0, CompletionPerM: 30.0},

		// Google models
		"gemini-2.0-flash":       {PromptPerM: 0.10, CompletionPerM: 0.40},
		"gemini-1.5-pro":         {PromptPerM: 1.25, CompletionPerM: 5.0},
		"gemini-2.0-flash-lite":  {PromptPerM: 0.075, CompletionPerM: 0.30},
		"gemini-2.5-flash-preview": {PromptPerM: 0.15, CompletionPerM: 0.60},
	}
}

// fetchOpenRouterPricing retrieves current pricing from OpenRouter API.
func (bt *BudgetTracker) fetchOpenRouterPricing(apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openrouter returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	for _, model := range result.Data {
		promptPrice, _ := strconv.ParseFloat(model.Pricing.Prompt, 64)
		completionPrice, _ := strconv.ParseFloat(model.Pricing.Completion, 64)

		// OpenRouter prices are per token, convert to per million
		bt.pricing[model.ID] = ModelPricing{
			PromptPerM:     promptPrice * 1_000_000,
			CompletionPerM: completionPrice * 1_000_000,
		}
	}

	return nil
}

// CanMakeRequest checks if another API request is allowed within budget.
func (bt *BudgetTracker) CanMakeRequest() error {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if bt.requestCount >= bt.maxRequests {
		return fmt.Errorf("request limit reached: %d/%d", bt.requestCount, bt.maxRequests)
	}
	if bt.spentUSD >= bt.budgetUSD {
		return fmt.Errorf("budget exceeded: $%.4f/$%.2f", bt.spentUSD, bt.budgetUSD)
	}
	return nil
}

// RecordRequest records token usage and calculates cost.
func (bt *BudgetTracker) RecordRequest(model string, promptTokens, completionTokens int) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	bt.requestCount++

	pricing, ok := bt.pricing[model]
	if !ok {
		// Use a default expensive pricing as a safety measure
		pricing = ModelPricing{PromptPerM: 10.0, CompletionPerM: 30.0}
	}

	cost := (float64(promptTokens) / 1_000_000 * pricing.PromptPerM) +
		(float64(completionTokens) / 1_000_000 * pricing.CompletionPerM)

	bt.spentUSD += cost
}

// RecordRequestFromResponse extracts token usage from a JSON response body.
func (bt *BudgetTracker) RecordRequestFromResponse(model string, responseBody []byte) {
	// Try Anthropic format
	if usage := gjson.GetBytes(responseBody, "usage"); usage.Exists() {
		promptTokens := int(gjson.GetBytes(responseBody, "usage.input_tokens").Int())
		completionTokens := int(gjson.GetBytes(responseBody, "usage.output_tokens").Int())
		bt.RecordRequest(model, promptTokens, completionTokens)
		return
	}

	// Try OpenAI format
	if usage := gjson.GetBytes(responseBody, "usage"); usage.Exists() {
		promptTokens := int(gjson.GetBytes(responseBody, "usage.prompt_tokens").Int())
		completionTokens := int(gjson.GetBytes(responseBody, "usage.completion_tokens").Int())
		bt.RecordRequest(model, promptTokens, completionTokens)
		return
	}

	// Try Gemini format
	if usage := gjson.GetBytes(responseBody, "usageMetadata"); usage.Exists() {
		promptTokens := int(gjson.GetBytes(responseBody, "usageMetadata.promptTokenCount").Int())
		completionTokens := int(gjson.GetBytes(responseBody, "usageMetadata.candidatesTokenCount").Int())
		bt.RecordRequest(model, promptTokens, completionTokens)
		return
	}

	// No usage found, record as 1 request with estimated tokens
	bt.RecordRequest(model, 500, 100)
}

// GetSpent returns the total amount spent so far.
func (bt *BudgetTracker) GetSpent() float64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return bt.spentUSD
}

// GetBudget returns the total budget.
func (bt *BudgetTracker) GetBudget() float64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return bt.budgetUSD
}

// GetRequestCount returns the number of requests made.
func (bt *BudgetTracker) GetRequestCount() int {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return bt.requestCount
}

// GetRemainingBudget returns the remaining budget.
func (bt *BudgetTracker) GetRemainingBudget() float64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return bt.budgetUSD - bt.spentUSD
}

// Summary returns a human-readable summary of budget usage.
func (bt *BudgetTracker) Summary() string {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return fmt.Sprintf("Budget: $%.4f/$%.2f (%.1f%%), Requests: %d/%d",
		bt.spentUSD, bt.budgetUSD,
		(bt.spentUSD/bt.budgetUSD)*100,
		bt.requestCount, bt.maxRequests)
}
