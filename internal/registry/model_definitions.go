// Package registry provides model definitions and lookup helpers for various AI providers.
// Static model metadata is loaded from the embedded models.json file and can be refreshed from network.
package registry

import (
	"strings"
)

// staticModelsJSON mirrors the top-level structure of models.json.
type staticModelsJSON struct {
	Claude      []*ModelInfo `json:"claude"`
	Gemini      []*ModelInfo `json:"gemini"`
	Vertex      []*ModelInfo `json:"vertex"`
	GeminiCLI   []*ModelInfo `json:"gemini-cli"`
	AIStudio    []*ModelInfo `json:"aistudio"`
	CodexFree   []*ModelInfo `json:"codex-free"`
	CodexTeam   []*ModelInfo `json:"codex-team"`
	CodexPlus   []*ModelInfo `json:"codex-plus"`
	CodexPro    []*ModelInfo `json:"codex-pro"`
	Qwen        []*ModelInfo `json:"qwen"`
	IFlow       []*ModelInfo `json:"iflow"`
	Kimi        []*ModelInfo `json:"kimi"`
	Antigravity []*ModelInfo `json:"antigravity"`
}

// GetClaudeModels returns the standard Claude model definitions.
func GetClaudeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Claude)
}

// GetGeminiModels returns the standard Gemini model definitions.
func GetGeminiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Gemini)
}

// GetGeminiVertexModels returns Gemini model definitions for Vertex AI.
func GetGeminiVertexModels() []*ModelInfo {
	return cloneModelInfos(getModels().Vertex)
}

// GetGeminiCLIModels returns Gemini model definitions for the Gemini CLI.
func GetGeminiCLIModels() []*ModelInfo {
	return cloneModelInfos(getModels().GeminiCLI)
}

// GetAIStudioModels returns model definitions for AI Studio.
func GetAIStudioModels() []*ModelInfo {
	return cloneModelInfos(getModels().AIStudio)
}

// GetCodexFreeModels returns model definitions for the Codex free plan tier.
func GetCodexFreeModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexFree)
}

// GetCodexTeamModels returns model definitions for the Codex team plan tier.
func GetCodexTeamModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexTeam)
}

// GetCodexPlusModels returns model definitions for the Codex plus plan tier.
func GetCodexPlusModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPlus)
}

// GetCodexProModels returns model definitions for the Codex pro plan tier.
func GetCodexProModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPro)
}

// GetQwenModels returns the standard Qwen model definitions.
func GetQwenModels() []*ModelInfo {
	return cloneModelInfos(getModels().Qwen)
}

// GetIFlowModels returns the standard iFlow model definitions.
func GetIFlowModels() []*ModelInfo {
	return cloneModelInfos(getModels().IFlow)
}

// GetKimiModels returns the standard Kimi (Moonshot AI) model definitions.
func GetKimiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Kimi)
}

// GetAntigravityModels returns the standard Antigravity model definitions.
func GetAntigravityModels() []*ModelInfo {
	return cloneModelInfos(getModels().Antigravity)
}

// cloneModelInfos returns a shallow copy of the slice with each element deep-cloned.
func cloneModelInfos(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*ModelInfo, len(models))
	for i, m := range models {
		out[i] = cloneModelInfo(m)
	}
	return out
}

// GetStaticModelDefinitionsByChannel returns static model definitions for a given channel/provider.
// It returns nil when the channel is unknown.
//
// Supported channels:
//   - claude
//   - gemini
//   - vertex
//   - gemini-cli
//   - aistudio
//   - codex
//   - qwen
//   - iflow
//   - kimi
//   - antigravity
func GetStaticModelDefinitionsByChannel(channel string) []*ModelInfo {
	key := strings.ToLower(strings.TrimSpace(channel))
	switch key {
	case "claude":
		return GetClaudeModels()
	case "gemini":
		return GetGeminiModels()
	case "vertex":
		return GetGeminiVertexModels()
	case "gemini-cli":
		return GetGeminiCLIModels()
	case "aistudio":
		return GetAIStudioModels()
	case "codex":
		return GetCodexProModels()
	case "qwen":
		return GetQwenModels()
	case "iflow":
		return GetIFlowModels()
	case "kimi":
		return GetKimiModels()
	case "antigravity":
		return GetAntigravityModels()
	default:
		return nil
	}
}

// GetPerplexityModels returns supported models for Perplexity Pro accounts.
func GetPerplexityModels() []*ModelInfo {
	entries := []struct {
		ID          string
		DisplayName string
		Description string
		Created     int64
	}{
		// Claude models
		{ID: "claude-4.5-sonnet", DisplayName: "Claude Sonnet 4.5", Description: "Anthropic Claude 4.5 Sonnet via Perplexity", Created: 1736899200},
		{ID: "claude-4.5-sonnet-thinking", DisplayName: "Claude Sonnet 4.5 (Thinking)", Description: "Claude 4.5 Sonnet with extended thinking", Created: 1736899200},
		{ID: "claude-4.5-opus", DisplayName: "Claude Opus 4.5", Description: "Anthropic Claude 4.5 Opus (max tier) via Perplexity", Created: 1736899200},
		{ID: "claude-4.5-opus-thinking", DisplayName: "Claude Opus 4.5 (Thinking)", Description: "Claude 4.5 Opus with extended thinking", Created: 1736899200},

		// GPT models
		{ID: "gpt-5.2", DisplayName: "GPT-5.2", Description: "OpenAI GPT-5.2 via Perplexity", Created: 1736899200},
		{ID: "gpt-5.2-thinking", DisplayName: "GPT-5.2 (Reasoning)", Description: "GPT-5.2 with reasoning/thinking mode", Created: 1736899200},

		// Gemini models
		{ID: "gemini-3-flash", DisplayName: "Gemini 3 Flash", Description: "Google Gemini 3 Flash via Perplexity", Created: 1736899200},
		{ID: "gemini-3-flash-thinking", DisplayName: "Gemini 3 Flash (Reasoning)", Description: "Gemini 3 Flash with reasoning mode", Created: 1736899200},
		{ID: "gemini-3-pro", DisplayName: "Gemini 3 Pro", Description: "Google Gemini 3 Pro via Perplexity", Created: 1736899200},

		// Grok models
		{ID: "grok-4.1", DisplayName: "Grok 4.1", Description: "xAI Grok 4.1 via Perplexity", Created: 1736899200},
		{ID: "grok-4.1-reasoning", DisplayName: "Grok 4.1 (Reasoning)", Description: "Grok 4.1 with reasoning mode", Created: 1736899200},

		// Other models
		{ID: "kimi-k2-thinking", DisplayName: "Kimi K2 Thinking", Description: "Moonshot Kimi K2 with thinking (hosted in US)", Created: 1736899200},

		// Perplexity native models
		{ID: "best", DisplayName: "Best", Description: "Perplexity's best model selection", Created: 1736899200},
		{ID: "sonar", DisplayName: "Sonar", Description: "Perplexity Sonar model", Created: 1736899200},
		{ID: "perplexity-pro", DisplayName: "Perplexity Pro", Description: "Perplexity Pro default model", Created: 1736899200},
	}

	var models []*ModelInfo
	for _, entry := range entries {
		models = append(models, &ModelInfo{
			ID:          entry.ID,
			Object:      "model",
			Created:     entry.Created,
			OwnedBy:     "perplexity",
			Type:        "perplexity",
			DisplayName: entry.DisplayName,
			Description: entry.Description,
		})
	}
	return models
}

// LookupStaticModelInfo searches all static model definitions for a model by ID.
// Returns nil if no matching model is found.
func LookupStaticModelInfo(modelID string) *ModelInfo {
	if modelID == "" {
		return nil
	}

	data := getModels()
	allModels := [][]*ModelInfo{
		data.Claude,
		data.Gemini,
		data.Vertex,
		data.GeminiCLI,
		data.AIStudio,
		data.CodexPro,
		data.Qwen,
		data.IFlow,
		data.Kimi,
		data.Antigravity,
		GetPerplexityModels(),
	}
	for _, models := range allModels {
		for _, m := range models {
			if m != nil && m.ID == modelID {
				return cloneModelInfo(m)
			}
		}
	}

	return nil
}
