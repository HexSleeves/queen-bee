package adapter

import (
	"github.com/HexSleeves/waggle/internal/safety"
)

// NewCodexAdapter creates a Codex CLI adapter.
func NewCodexAdapter(command string, args []string, workDir string, guard *safety.Guard) *CLIAdapter {
	if command == "" {
		command = "codex"
	}
	if len(args) == 0 {
		args = []string{"exec"}
	}
	return NewCLIAdapter(CLIAdapterConfig{
		Name:    "codex",
		Command: command,
		Args:    args,
		WorkDir: workDir,
		Guard:   guard,
		Mode:    PromptAsArg,
	})
}
