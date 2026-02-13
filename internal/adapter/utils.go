package adapter

import (
	"errors"
	"os/exec"
)

// getExitCode extracts the exit code from an error.
// Returns -1 if the error is nil or doesn't have an exit code.
func getExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return -1
}
