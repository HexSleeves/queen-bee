package adapter

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"

	"github.com/exedev/queen-bee/internal/task"
	"github.com/exedev/queen-bee/internal/worker"
)

// ShelleyAdapter wraps the `shelley` CLI
type ShelleyAdapter struct {
	command string
	args    []string
	workDir string
}

func NewShelleyAdapter(command string, args []string, workDir string) *ShelleyAdapter {
	if command == "" {
		command = "shelley"
	}
	if len(args) == 0 {
		args = []string{"-p"}
	}
	return &ShelleyAdapter{
		command: command,
		args:    args,
		workDir: workDir,
	}
}

func (a *ShelleyAdapter) Name() string { return "shelley" }

func (a *ShelleyAdapter) Available() bool {
	_, err := exec.LookPath(a.command)
	return err == nil
}

func (a *ShelleyAdapter) CreateWorker(id string) worker.Bee {
	return &ShelleyWorker{
		id:      id,
		adapter: a,
		status:  worker.StatusIdle,
	}
}

// ShelleyWorker is a Bee backed by the shelley CLI
type ShelleyWorker struct {
	id      string
	adapter *ShelleyAdapter
	status  worker.Status
	result  *task.Result
	output  strings.Builder
	cmd     *exec.Cmd
	mu      sync.Mutex
}

func (w *ShelleyWorker) ID() string   { return w.id }
func (w *ShelleyWorker) Type() string { return "shelley" }

func (w *ShelleyWorker) Spawn(ctx context.Context, t *task.Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	prompt := buildPrompt(t)

	// Build command: shelley -p "prompt"
	args := make([]string, len(w.adapter.args))
	copy(args, w.adapter.args)
	args = append(args, prompt)

	w.cmd = exec.CommandContext(ctx, w.adapter.command, args...)
	if w.adapter.workDir != "" {
		w.cmd.Dir = w.adapter.workDir
	}

	var stdout, stderr bytes.Buffer
	w.cmd.Stdout = &stdout
	w.cmd.Stderr = &stderr

	w.status = worker.StatusRunning

	// Run async
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
				Errors:  []string{err.Error(), stderr.String()},
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

func (w *ShelleyWorker) Monitor() worker.Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *ShelleyWorker) Result() *task.Result {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.result
}

func (w *ShelleyWorker) Kill() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		w.status = worker.StatusFailed
		return w.cmd.Process.Kill()
	}
	return nil
}

func (w *ShelleyWorker) Output() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.String()
}
