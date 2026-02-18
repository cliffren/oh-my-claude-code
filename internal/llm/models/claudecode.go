package models

const (
	ProviderClaudeCode ModelProvider = "claude-code"

	ClaudeCodeSonnet ModelID = "claude-code-sonnet"
	ClaudeCodeOpus   ModelID = "claude-code-opus"
	ClaudeCodeHaiku  ModelID = "claude-code-haiku"
)

var ClaudeCodeModels = map[ModelID]Model{
	ClaudeCodeSonnet: {
		ID:                  ClaudeCodeSonnet,
		Name:                "Claude Code Sonnet",
		Provider:            ProviderClaudeCode,
		APIModel:            "sonnet",
		CostPer1MIn:         3.0,
		CostPer1MOut:        15.0,
		CostPer1MInCached:   0.30,
		CostPer1MOutCached:  0,
		ContextWindow:       200000,
		DefaultMaxTokens:    16384,
		CanReason:           true,
		SupportsAttachments: true,
	},
	ClaudeCodeOpus: {
		ID:                  ClaudeCodeOpus,
		Name:                "Claude Code Opus",
		Provider:            ProviderClaudeCode,
		APIModel:            "opus",
		CostPer1MIn:         15.0,
		CostPer1MOut:        75.0,
		CostPer1MInCached:   1.50,
		CostPer1MOutCached:  0,
		ContextWindow:       200000,
		DefaultMaxTokens:    16384,
		CanReason:           true,
		SupportsAttachments: true,
	},
	ClaudeCodeHaiku: {
		ID:                  ClaudeCodeHaiku,
		Name:                "Claude Code Haiku",
		Provider:            ProviderClaudeCode,
		APIModel:            "haiku",
		CostPer1MIn:         0.80,
		CostPer1MOut:        4.0,
		CostPer1MInCached:   0.08,
		CostPer1MOutCached:  0,
		ContextWindow:       200000,
		DefaultMaxTokens:    8192,
		CanReason:           false,
		SupportsAttachments: true,
	},
}
