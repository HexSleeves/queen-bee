package adapter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/exedev/queen-bee/internal/task"
	"github.com/exedev/queen-bee/internal/worker"
)

// ExecAdapter runs tasks as shell commands directly.
// Useful as a fallback when no AI CLI is available, or for
// tasks that are pure shell operations (tests, builds, linting).
type ExecAdapter struct {
	shell   string
	workDir string
}

func NewExecAdapter(workDir string) *ExecAdapter {
	shell := "/bin/bash"
	if s, err := exec.LookPath("bash"); err == nil {
		shell = s
	}
	return &ExecAdapter{
		shell:   shell,
		workDir: workDir,
	}
}

func (a *ExecAdapter) Name() string    { return "exec" }
func (a *ExecAdapter) Available() bool { return true }

func (a *ExecAdapter) CreateWorker(id string) worker.Bee {
	return &ExecWorker{
		id:      id,
		adapter: a,
		status:  worker.StatusIdle,
	}
}

// ExecWorker runs a task's description as a shell script
type ExecWorker struct {
	id      string
	adapter *ExecAdapter
	status  worker.Status
	result  *task.Result
	output  strings.Builder
	cmd     *exec.Cmd
	mu      sync.Mutex
}

func (w *ExecWorker) ID() string   { return w.id }
func (w *ExecWorker) Type() string { return "exec" }

func (w *ExecWorker) Spawn(ctx context.Context, t *task.Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// For exec adapter, the task description should contain the shell command(s)
	// If context has a "command" key, use that instead
	script := t.Description
	if cmd, ok := t.Context["command"]; ok {
		script = cmd
	}

	w.cmd = exec.CommandContext(ctx, w.adapter.shell, "-c", script)
	if w.adapter.workDir != "" {
		w.cmd.Dir = w.adapter.workDir
	}

	var stdout, stderr bytes.Buffer
	w.cmd.Stdout = &stdout
	w.cmd.Stderr = &stderr

	w.status = worker.StatusRunning

	go func() {
		err := w.cmd.Run()

		w.mu.Lock()
		defer w.mu.Unlock()

		w.output.WriteString(stdout.String())
		if stderr.Len() > 0 {
			w.output.WriteString("\n[STDERR]\n")
			w.output.WriteString(stderr.String())
		}

		if err != nil {
			w.status = worker.StatusFailed
			w.result = &task.Result{
				Success: false,
				Output:  stdout.String(),
				Errors:  []string{fmt.Sprintf("%v", err), stderr.String()},
			}
		} else {
			w.status = worker.StatusComplete
			w.result = &task.Result{
				Success: true,
				Output:  stdout.String(),
			}
		}
	}()

	return nil
}

func (w *ExecWorker) Monitor() worker.Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *ExecWorker) Result() *task.Result {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.result
}

func (w *ExecWorker) Kill() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		w.status = worker.StatusFailed
		return w.cmd.Process.Kill()
	}
	return nil
}

func (w *ExecWorker) Output() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.String()
}
