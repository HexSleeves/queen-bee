<p align="center">
  <img src="assets/banner.svg" alt="Waggle" width="800">
</p>

<p align="center">
  <strong>Multi-agent orchestration through the waggle dance.</strong>
</p>

---

## What is Waggle?

Waggle is a multi-agent orchestration framework written in Go. A central **Queen** agent — powered by
any LLM with tool-use support — decomposes complex objectives into a graph of tasks, delegates them
to **Worker Bee** sub-agents (AI coding CLIs like Claude Code, Kimi, Codex, Gemini, OpenCode, or plain
shell commands), monitors execution in parallel, reviews results, and replans when needed.

The Queen runs as an **autonomous tool-using LLM agent**: she receives an objective, decides what
tasks to create, assigns them to workers, waits for results, reviews output, and declares completion
— all through tool calls. The Go code just executes tools and feeds results back.

The entire lifecycle is modeled after the [waggle dance](https://en.wikipedia.org/wiki/Waggle_dance),
the figure-eight dance honeybees use to communicate exactly where resources are and how to get them.

## How it Works

In **agent mode** (default), the Queen is an autonomous LLM that orchestrates everything through tool calls:

| Step | What Happens |
| ---- | ------------ |
| 🎯 Receive objective | Queen gets the user's goal and project context |
| 📋 Create tasks | Queen calls `create_tasks` to build a dependency graph |
| 🐝 Assign workers | Queen calls `assign_task` to dispatch work to CLI agents |
| ⏳ Wait for results | Queen calls `wait_for_workers` to block until workers finish |
| 🔍 Review output | Queen calls `get_task_output`, then `approve_task` or `reject_task` |
| 🔄 Iterate | Queen creates more tasks or assigns retries as needed |
| ✅ Complete | Queen calls `complete` when the objective is satisfied |

The Queen also has `read_file` and `list_files` tools to inspect the project directly.

A **legacy mode** (`--legacy`) is available that uses a structured Plan → Delegate → Monitor → Review → Replan loop instead of autonomous tool use.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       USER OBJECTIVE                            │
│              "Refactor the auth module to use JWT"              │
└──────────────────────────────┬──────────────────────────────────┘
                               │
                    ┌──────────▼──────────┐
                    │     👑 THE QUEEN     │
                    │   (tool-using LLM)  │
                    │                     │
                    │  create_tasks       │
                    │  assign_task        │
                    │  wait_for_workers   │
                    │  approve / reject   │
                    │  read_file          │
                    │  complete / fail    │
                    └───┬─────┬─────┬─────┘
                        │     │     │
              ┌─────────┘     │     └─────────┐
              ▼               ▼               ▼
     ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
     │ 🐝 Worker Bee │ │ 🐝 Worker Bee │ │ 🐝 Worker Bee │
     │  (kimi)      │ │   (codex)    │ │   (exec)     │
     └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
            │                │                │
     ┌──────▼────────────────▼────────────────▼──────┐
     │              🍯 THE HIVE (.hive/)              │
     │           SQLite state · Blackboard            │
     └────────────────────────────────────────────────┘

     ┌────────────────────────────────────────────────┐
     │              🖥️  TUI Dashboard                  │
     │   👑 Queen panel  │  📋 Task panel  │  Status   │
     └────────────────────────────────────────────────┘
```

## Quick Start

```bash
# Build
go build -o waggle ./cmd/waggle/

# Initialize a hive in your project
cd /path/to/your/project
waggle init

# Set your Queen's LLM provider in waggle.json
# (anthropic, openai, codex, gemini-api — any provider with tool-use support)

# Run with an objective — Queen plans + workers execute
waggle run "Refactor the auth module to use JWT tokens"

# Specify a worker adapter and worker count
waggle --adapter kimi --workers 8 run "Add unit tests for all handlers"

# Use the exec adapter for raw shell commands (no AI CLI needed)
waggle --adapter exec run "go test ./..."

# Pre-defined tasks from a file (skips Queen planning)
waggle --adapter exec --tasks tasks.json run "Run analysis pipeline"

# Plain log output instead of TUI
waggle --plain run "Review the codebase"

# Force legacy orchestration loop (no agent mode)
waggle --legacy run "Fix all lint warnings"

# Check session status
waggle status

# Resume a previous session
waggle resume <session-id>
```

When running in a terminal, Waggle displays a **TUI dashboard** showing the Queen's reasoning, tool calls, task progress, and worker status in real time. Use `--plain` for CI or piped output.

## Task File Format

Pre-define parallel tasks with dependencies in JSON:

```json
[
  {
    "id": "lint",
    "type": "test",
    "title": "Run linter",
    "description": "golangci-lint run ./...",
    "priority": 2
  },
  {
    "id": "test",
    "type": "test",
    "title": "Run tests",
    "description": "go test -race ./...",
    "priority": 3,
    "depends_on": ["lint"]
  },
  {
    "id": "build",
    "type": "code",
    "title": "Build binary",
    "description": "go build -o waggle ./cmd/waggle/",
    "priority": 1,
    "depends_on": ["test"]
  }
]
```

Tasks respect dependency ordering — `test` won't start until `lint` completes. Independent tasks run in parallel up to the configured worker limit.

## Worker Adapters

Waggle wraps coding agent CLIs behind a uniform interface. The `workers.default_adapter` config controls which adapter is used for all task assignments.

| Adapter | CLI | Invocation | Notes |
| ------- | ---------- | ----------- | ----------- |
| `claude-code` | Claude Code | `claude -p "<prompt>"` | |
| `kimi` | Kimi Code | `kimi --print --final-message-only -p "<prompt>"` | Fast (~60s/task) |
| `gemini` | Gemini CLI | `echo "<prompt>" \| gemini` | Pipe-based |
| `codex` | Codex | `codex exec "<prompt>"` | |
| `opencode` | OpenCode | `opencode run "<prompt>"` | |
| `exec` | Shell | `bash -c "<description>"` | No AI — runs commands directly |

Adapters are configured in `waggle.json` and can be customized with arbitrary commands, args, and environment variables.

## Queen LLM Providers

The Queen's own LLM is separate from worker adapters. Providers with **tool-use support** enable agent mode; CLI-based providers fall back to legacy mode.

| Provider | Config Value | Tool Use | Notes |
| -------- | ------------ | -------- | ----- |
| Anthropic API | `"anthropic"` | ✅ | Needs `ANTHROPIC_API_KEY` |
| OpenAI API | `"openai"` | ✅ | Needs `OPENAI_API_KEY` |
| Codex (OpenAI) | `"codex"` | ✅ | Uses OpenAI-compatible API |
| Gemini API | `"gemini-api"` | ✅ | Needs `GEMINI_API_KEY` |
| Kimi CLI | `"kimi"` | ❌ | Legacy mode only |
| Claude CLI | `"claude-cli"` | ❌ | Legacy mode only |
| Gemini CLI | `"gemini"` | ❌ | Legacy mode only |
| OpenCode CLI | `"opencode"` | ❌ | Legacy mode only |

## Configuration

Running `waggle init` creates a `waggle.json` configuration file:

```json
{
  "project_dir": ".",
  "hive_dir": ".hive",
  "queen": {
    "provider": "anthropic",
    "model": "claude-sonnet-4-20250514",
    "max_iterations": 50,
    "plan_timeout": 300000000000,
    "review_timeout": 120000000000,
    "compact_after_messages": 100
  },
  "workers": {
    "max_parallel": 4,
    "default_timeout": 600000000000,
    "max_retries": 2,
    "default_adapter": "claude-code"
  },
  "adapters": {
    "claude-code": { "command": "claude", "args": ["-p"] },
    "kimi":        { "command": "kimi",   "args": ["--print", "--final-message-only", "-p"] },
    "gemini":      { "command": "gemini" },
    "codex":       { "command": "codex",  "args": ["exec"] },
    "opencode":    { "command": "opencode", "args": ["run"] },
    "exec":        { "command": "bash" }
  },
  "safety": {
    "allowed_paths": ["."],
    "blocked_commands": ["rm -rf /", "sudo rm"],
    "read_only_mode": false,
    "max_file_size": 10485760
  }
}
```

| Section | Key Fields | Description |
| ------- | ---------- | ----------- |
| `queen` | `provider`, `model` | The Queen's own LLM. See [Queen LLM Providers](#queen-llm-providers) |
| `queen` | `api_key`, `base_url` | API credentials (can also use env vars like `ANTHROPIC_API_KEY`) |
| `queen` | `max_iterations` | Hard cap on the agent loop turns |
| `queen` | `compact_after_messages` | Compact conversation history after N messages |
| `workers` | `max_parallel` | Size of the worker pool (the swarm) |
| `workers` | `default_adapter` | Which adapter workers use for task execution |
| `workers` | `max_retries` | How many times a failed task is retried before giving up |
| `safety` | `allowed_paths` | Directories workers may touch (resolved relative to project root) |
| `safety` | `blocked_commands` | Substring patterns that will be rejected by the safety guard |

## Project Structure

```bash
waggle/
├── cmd/waggle/              # CLI entry point
│   ├── main.go
│   ├── app.go               # urfave/cli app definition + flags
│   ├── commands.go           # init, run, resume, config handlers
│   ├── status.go             # session / task status display
│   └── tasks.go              # task file loader
├── internal/
│   ├── queen/               # 👑 The Queen — orchestration
│   │   ├── queen.go          #   Legacy Plan → Delegate → Monitor → Review loop
│   │   ├── agent.go          #   Agent mode — autonomous tool-using LLM loop
│   │   ├── tools.go          #   Tool definitions + handlers (11 tools)
│   │   ├── prompt.go         #   System prompt builder for agent mode
│   │   ├── review.go         #   LLM-backed result evaluation
│   │   └── replan.go         #   LLM-backed replanning
│   ├── llm/                 # 🧠 Queen's LLM client
│   │   ├── client.go         #   Client + ToolClient interfaces
│   │   ├── types.go          #   ToolDef, ToolCall, ToolResult, Response types
│   │   ├── anthropic.go      #   Anthropic API (tool-use)
│   │   ├── openai.go         #   OpenAI-compatible API (tool-use)
│   │   ├── gemini.go         #   Google Gemini API (tool-use)
│   │   ├── cli.go            #   CLI-based LLM wrapper (no tool-use)
│   │   └── factory.go        #   Provider factory
│   ├── tui/                 # 🖥️ Terminal dashboard
│   │   ├── model.go          #   Bubble Tea model + update loop
│   │   ├── view.go           #   Queen panel, task panel, status bar
│   │   ├── events.go         #   TUI message types
│   │   ├── bridge.go         #   Log → TUI message routing + buffering
│   │   └── styles.go         #   Lipgloss styles
│   ├── worker/              # 🐝 Worker pool manager
│   │   └── worker.go
│   ├── adapter/             # CLI wrapper adapters
│   │   ├── adapter.go        #   Registry + TaskRouter
│   │   ├── claude.go         #   Claude Code
│   │   ├── kimi.go           #   Kimi Code
│   │   ├── gemini.go         #   Gemini CLI
│   │   ├── codex.go          #   Codex
│   │   ├── opencode.go       #   OpenCode
│   │   └── exec.go           #   Direct shell execution
│   ├── task/                # 📌 Task graph with dependency tracking
│   │   └── task.go
│   ├── state/               # 💾 SQLite persistence (WAL mode)
│   │   └── db.go
│   ├── bus/                 # 📨 In-process pub/sub message bus
│   │   └── bus.go
│   ├── blackboard/          # 📋 Shared memory for inter-agent comms
│   │   └── blackboard.go
│   ├── safety/              # 🛡️ Path restriction + command filtering
│   │   └── safety.go
│   ├── config/              # ⚙️ Configuration management
│   │   └── config.go
│   ├── compact/             # 📦 Context window compaction
│   │   └── compact.go
│   └── errors/              # Error classification + retry logic
│       └── errors.go
├── waggle.json              # Configuration file
├── go.mod
└── README.md
```

## Queen's Tools

In agent mode, the Queen has 11 tools:

| Tool | Purpose |
|------|---------|
| `create_tasks` | Create tasks in the graph with types, priorities, dependencies, constraints, allowed paths |
| `assign_task` | Assign a pending task to a worker (checks deps, pool capacity, uses configured adapter) |
| `wait_for_workers` | Block until one or more workers complete (with configurable timeout) |
| `get_status` | Get current status of all tasks |
| `get_task_output` | Read a completed or failed task's full output |
| `approve_task` | Mark a task as approved |
| `reject_task` | Reject a task with feedback, re-queue for retry |
| `read_file` | Read a file from the project directory (safety-checked) |
| `list_files` | List directory contents with file sizes |
| `complete` | Declare the objective complete with a summary |
| `fail` | Declare the objective failed with a reason |

## Persistence

All state lives in the `.hive/` directory:

```bash
.hive/
└── hive.db          # SQLite database (WAL mode)
```

The database stores:

- **Sessions** — objective, status, phase, iteration count, timestamps
- **Tasks** — full task state including results, retries, error history
- **Events** — append-only log of every bus message for auditability
- **KV store** — agent conversation turns for session resume

SQLite with WAL mode allows concurrent reads while the Queen writes, with a busy timeout of 5 seconds for contention handling. Sessions can be resumed after interruption with `waggle resume <session-id>`.

## Safety

The **Safety Guard** (`internal/safety`) enforces constraints on every worker operation:

- **Path allowlist** — workers can only touch files within configured directories. Paths are resolved to absolute form and checked with prefix matching. By default, only the project root is allowed.
- **Command blocklist** — commands are checked against substring patterns (e.g., `rm -rf /`, `sudo rm`). Any match is rejected before execution.
- **File size limits** — prevents workers from reading or writing files above a configurable threshold (default: 10 MB).
- **Read-only mode** — when enabled, blocks all write operations.
- **Scope constraints** — every task gets injected rules: no out-of-scope changes, no unsolicited refactoring, no signature changes.

The guard is injected into every adapter invocation, so safety is enforced regardless of which CLI backend is in use.

## License

MIT
