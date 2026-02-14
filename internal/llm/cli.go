package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CLIClient uses an external CLI tool (kimi, claude, gemini, etc.) as the LLM.
// This requires no API key — it reuses whatever CLI the workers use.
type CLIClient struct {
	command string
	args    []string // args before the prompt, e.g. ["--print", "-p"]
	workDir string
	pipe    bool // if true, pipe prompt to stdin instead of appending as arg
}

// NewCLIClient creates a Client backed by a CLI tool.
// For kimi:   NewCLIClient("kimi", []string{"--print", "--final-message-only", "-p"}, dir, false)
// For claude: NewCLIClient("claude", []string{"-p"}, dir, false)
// For gemini: NewCLIClient("gemini", nil, dir, true)  // pipes to stdin
func NewCLIClient(command string, args []string, workDir string, pipe bool) *CLIClient {
	return &CLIClient{
		command: command,
		args:    args,
		workDir: workDir,
		pipe:    pipe,
	}
}

func (c *CLIClient) Chat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	// Combine system + user into a single prompt for CLI tools
	var prompt strings.Builder
	if systemPrompt != "" {
		prompt.WriteString(systemPrompt)
		prompt.WriteString("\n\n")
	}
	prompt.WriteString(userMessage)
	return c.run(ctx, prompt.String())
}

func (c *CLIClient) ChatWithHistory(ctx context.Context, systemPrompt string, messages []Message) (string, error) {
	// Flatten history into a single prompt for CLI tools
	var prompt strings.Builder
	if systemPrompt != "" {
		prompt.WriteString(systemPrompt)
		prompt.WriteString("\n\n")
	}
	for _, m := range messages {
		prompt.WriteString(fmt.Sprintf("[%s]: %s\n\n", m.Role, m.Content))
	}
	return c.run(ctx, prompt.String())
}

func (c *CLIClient) run(ctx context.Context, prompt string) (string, error) {
	var args []string
	args = append(args, c.args...)

	var cmd *exec.Cmd
	if c.pipe {
		// Pipe prompt to stdin (gemini style)
		cmd = exec.CommandContext(ctx, c.command, args...)
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		// Append prompt as final arg (kimi/claude style)
		args = append(args, prompt)
		cmd = exec.CommandContext(ctx, c.command, args...)
	}

	if c.workDir != "" {
		cmd.Dir = c.workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w (stderr: %s)", c.command, err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}
