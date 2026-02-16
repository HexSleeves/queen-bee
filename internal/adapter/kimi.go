package adapter

import (
	"os"

	"github.com/HexSleeves/waggle/internal/safety"
)

// NewKimiAdapter creates a Kimi Code CLI adapter.
func NewKimiAdapter(command string, args []string, workDir string, guard *safety.Guard) *CLIAdapter {
	if command == "" {
		command = "kimi"
	}
	if len(args) == 0 {
		args = []string{"--print", "--final-message-only", "-p"}
	}
	return NewCLIAdapter(CLIAdapterConfig{
		Name:    "kimi",
		Command: command,
		Args:    args,
		WorkDir: workDir,
		Guard:   guard,
		Mode:    PromptAsArg,
		FallbackPaths: []string{
			os.ExpandEnv("$HOME/.local/bin/kimi"),
			"/usr/local/bin/kimi",
		},
	})
}
