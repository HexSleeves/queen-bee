# Agent Instructions for Waggle

## Before Committing

Always perform these steps before creating a commit:

1. **Format**: Run `gofmt -w .` to format all Go files.
2. **Lint**: Run `go vet ./...` and fix all reported issues.
3. **Test**: Run `go test ./...` and ensure all tests pass.
4. **Build**: Run `go build ./cmd/waggle/` to confirm compilation.
5. **Update TODO.md**: Mark completed items as done, add new items if work created them.
6. **Commit & Push**: Write a descriptive commit message, then `git push`.

## Code Style

- Follow standard Go conventions (gofmt, go vet).
- Tests go in `_test.go` files alongside the code they test.
- Use `t.Helper()` in test helpers.
- Use `t.TempDir()` or `os.MkdirTemp` for test directories.

## Project Structure

See README.md and ARCHITECTURE.md for full details. Key packages:

- `cmd/waggle/` — CLI entry point (run, init, status, sessions, logs, dag, kill, resume, config)
- `internal/queen/` — Orchestration (agent.go, queen.go, tools.go, reporter.go, gitstate.go)
- `internal/adapter/` — CLI adapters (generic.go is the base)
- `internal/llm/` — Provider-agnostic LLM client (anthropic.go, openai.go, gemini.go, retry.go)
- `internal/output/` — Output mode manager + pterm Printer wrapper
- `internal/state/` — SQLite persistence (WAL mode, reader/writer pools)
- `internal/task/` — Task graph with dependency tracking + DAG visualization
- `internal/tui/` — Bubble Tea TUI with progress bar + DAG panel

## Key Design Notes

- `q.logger` = internal warnings to stderr (always active)
- `q.Printer()` = user-facing styled output via pterm (only active in `--plain` mode)
- TUI mode has its own rendering pipeline; Printer is a no-op
- Session IDs are 8-char base32 random strings
- v0.1.0 released via GoReleaser
