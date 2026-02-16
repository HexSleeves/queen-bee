package adapter

import (
	"os/exec"

	"github.com/HexSleeves/waggle/internal/safety"
)

// NewExecAdapter creates a direct shell execution adapter.
// Tasks' descriptions are run as bash scripts (no AI CLI involved).
func NewExecAdapter(workDir string, guard *safety.Guard) *CLIAdapter {
	shell := "/bin/bash"
	if s, err := exec.LookPath("bash"); err == nil {
		shell = s
	}
	return NewCLIAdapter(CLIAdapterConfig{
		Name:    "exec",
		Command: shell,
		WorkDir: workDir,
		Guard:   guard,
		Mode:    PromptAsScript,
	})
}
