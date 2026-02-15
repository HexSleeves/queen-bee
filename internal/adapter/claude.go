package adapter

import (
	"github.com/exedev/waggle/internal/safety"
)

// NewClaudeAdapter creates a Claude Code CLI adapter.
func NewClaudeAdapter(command string, args []string, workDir string, guard *safety.Guard) *CLIAdapter {
	if command == "" {
		command = "claude"
	}
	if len(args) == 0 {
		args = []string{"-p"}
	}
	return NewCLIAdapter(CLIAdapterConfig{
		Name:    "claude-code",
		Command: command,
		Args:    args,
		WorkDir: workDir,
		Guard:   guard,
		Mode:    PromptAsArg,
	})
}
