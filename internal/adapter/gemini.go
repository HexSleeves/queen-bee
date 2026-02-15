package adapter

import (
	"os"

	"github.com/exedev/waggle/internal/safety"
)

// NewGeminiAdapter creates a Gemini CLI adapter.
// Gemini uses stdin for prompt input instead of command-line arguments.
func NewGeminiAdapter(command string, args []string, workDir string, guard *safety.Guard) *CLIAdapter {
	if command == "" {
		command = "gemini"
	}
	return NewCLIAdapter(CLIAdapterConfig{
		Name:    "gemini",
		Command: command,
		Args:    args,
		WorkDir: workDir,
		Guard:   guard,
		Mode:    PromptOnStdin,
		FallbackPaths: []string{
			os.ExpandEnv("$HOME/.bun/bin/gemini"),
			"/usr/local/bin/gemini",
		},
	})
}
