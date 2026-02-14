package llm

import "fmt"

// ProviderConfig holds what's needed to construct an LLM client.
type ProviderConfig struct {
	Provider string // "anthropic", "openai", "gemini", "kimi", "claude-cli", or any CLI name
	Model    string
	APIKey   string
	WorkDir  string // for CLI-based providers
}

// NewFromConfig creates the appropriate Client based on provider name.
func NewFromConfig(cfg ProviderConfig) (Client, error) {
	switch cfg.Provider {
	case "anthropic":
		return NewAnthropicClient(cfg.APIKey, cfg.Model), nil

	case "kimi":
		return NewCLIClient("kimi", []string{"--print", "--final-message-only", "-p"}, cfg.WorkDir, false), nil

	case "claude-cli", "claude-code":
		return NewCLIClient("claude", []string{"-p"}, cfg.WorkDir, false), nil

	case "gemini":
		return NewCLIClient("gemini", nil, cfg.WorkDir, true), nil

	case "opencode":
		return NewCLIClient("opencode", []string{"run"}, cfg.WorkDir, false), nil

	case "":
		return nil, fmt.Errorf("no LLM provider configured (set queen.provider in queen.json)")

	default:
		return nil, fmt.Errorf("unknown LLM provider: %q", cfg.Provider)
	}
}
