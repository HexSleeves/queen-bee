package adapter

import (
	"os"

	"github.com/exedev/waggle/internal/safety"
)

// NewOpenCodeAdapter creates an OpenCode CLI adapter.
func NewOpenCodeAdapter(command string, args []string, workDir string, guard *safety.Guard) *CLIAdapter {
	if command == "" {
		command = "opencode"
	}
	if len(args) == 0 {
		args = []string{"run"}
	}
	return NewCLIAdapter(CLIAdapterConfig{
		Name:    "opencode",
		Command: command,
		Args:    args,
		WorkDir: workDir,
		Guard:   guard,
		Mode:    PromptAsArg,
		FallbackPaths: []string{
			os.ExpandEnv("$HOME/.opencode/bin/opencode"),
			"/usr/local/bin/opencode",
		},
	})
}
