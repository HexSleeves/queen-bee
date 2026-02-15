# Queen Bee — Project Context

> Last updated: 2026-02-14

## What This Is

A multi-agent orchestration framework in Go. A central **Queen** agent decomposes objectives into tasks, delegates them to **Worker Bee** sub-agents running via coding CLI tools, monitors execution, reviews results with LLM judgment, and reports findings back to the user.

Think of it as a task runner where the tasks are executed by AI coding agents in parallel, with an AI reviewer ensuring quality.

## Architecture

```
User Objective
       │
   ┌───▼───┐
   │ Queen │  Plan → Delegate → Monitor → Review (LLM) → Replan (LLM)
   └───┬───┘
       │ spawns via adapters (with safety guard + scope constraints)
   ┌───┴────────────┬──────────────┐
   ▼                ▼              ▼
┌──────┐      ┌──────┐      ┌──────┐
│Worker│      │Worker│      │Worker│   (parallel, ephemeral)
│(kimi)│      │(kimi)│      │(exec)│
└──┬───┘      └──┬───┘      └──┬───┘
   │              │              │
   └──────────────┴──────────────┘
                  │
            ┌─────▼─────┐
            │ Blackboard │  shared results
            │  SQLite DB │  persistent state
            │  Event Log │  append-only audit
            └───────────┘
```

## Module Map

| Package | File(s) | Lines | Purpose |
|---------|---------|-------|---------|
| `cmd/queen-bee` | `main.go`, `app.go`, `commands.go`, `status.go`, `tasks.go` | ~530 | CLI entry point (urfave/cli): `run`, `init`, `status`, `config`, `resume` |
| `internal/queen` | `queen.go` | ~1260 | **Core orchestrator** — Plan/Delegate/Monitor/Review/Replan loop |
| `internal/queen` | `review.go` | ~205 | LLM-backed review: evaluates worker output quality |
| `internal/queen` | `replan.go` | ~140 | LLM-backed replan: identifies follow-up tasks |
| `internal/llm` | `client.go`, `anthropic.go`, `cli.go`, `factory.go` | ~210 | **Provider-agnostic LLM client** (Anthropic SDK, CLI adapters) |
| `internal/worker` | `worker.go` | ~150 | `Bee` interface + concurrent `Pool` with limits |
| `internal/adapter` | 6 adapters + utils | ~3200 | CLI wrappers: claude, codex, opencode, kimi, gemini, exec |
| `internal/adapter` | `adapter.go` | ~100 | `Registry` + `TaskRouter` (maps task types → adapters) |
| `internal/bus` | `bus.go` | ~110 | In-process pub/sub message bus with panic-safe handler dispatch |
| `internal/blackboard` | `blackboard.go` | ~165 | Shared memory — workers post results, Queen reads (deep-copy on History) |
| `internal/state` | `db.go` | ~510 | **SQLite persistence** — sessions, events, tasks, blackboard, kv |
| `internal/state` | `state.go` | ~180 | Legacy JSONL append-only event log (still writes in parallel) |
| `internal/task` | `task.go` | ~270 | Task graph with dependency tracking, priority, status, **cycle detection** |
| `internal/config` | `config.go` | ~140 | Configuration with defaults, JSON serialization |
| `internal/safety` | `safety.go` | ~110 | Path allowlisting, command blocklisting — **enforced in all adapters** |
| `internal/compact` | `compact.go` | ~135 | Context window management, token estimation, summarization |
| `internal/errors` | `errors.go` | ~330 | Error classification, retry/permanent types, backoff |

**Total: ~5800 lines of source across 27 Go files + ~6800 lines of tests**

## Key Interfaces

### `worker.Bee` — What every worker must implement
```go
type Bee interface {
    ID() string
    Type() string
    Spawn(ctx context.Context, t *task.Task) error
    Monitor() Status  // idle, running, stuck, complete, failed
    Result() *task.Result
    Kill() error
    Output() string
}
```

### `adapter.Adapter` — How CLIs are wrapped
```go
type Adapter interface {
    Name() string
    Available() bool
    CreateWorker(id string) worker.Bee
}
```

### `llm.Client` — Provider-agnostic LLM interface
```go
type Client interface {
    Chat(ctx context.Context, systemPrompt, userMessage string) (string, error)
    ChatWithHistory(ctx context.Context, systemPrompt string, messages []Message) (string, error)
}
```

Implementations: `AnthropicClient` (SDK), `CLIClient` (wraps any CLI tool).

## Queen's LLM Intelligence

The Queen now has her own LLM for reasoning, separate from the worker adapters.

### Review Phase
After each task completes, the Queen's LLM evaluates the output:
- **Scope** — did the worker stay within constraints?
- **Correctness** — is the output technically correct?
- **Follow-up** — are there obvious next steps?

Returns a `ReviewVerdict` (approved/rejected with reason, suggestions, new task ideas). Rejected tasks are re-queued with feedback appended.

### Replan Phase
After all tasks complete, the Queen's LLM reviews the full picture:
- What was the objective?
- What was completed, what failed?
- Are additional tasks needed?

Returns new tasks to add (or empty array → done).

### Provider Selection
Configured via `queen.json`:
```json
{"queen": {"provider": "kimi"}}        // uses kimi CLI (free, no API key)
{"queen": {"provider": "gemini"}}       // uses gemini CLI
{"queen": {"provider": "claude-cli"}}   // uses claude CLI
{"queen": {"provider": "anthropic"}}    // uses Anthropic API (needs key)
```
No provider = review/replan disabled, old exit-code-based behavior.

## Scope Constraints System

Three layers control what workers can and cannot do:

1. **Plan prompt** — The Queen instructs the planner to produce narrowly-scoped tasks with `constraints` (what NOT to do) and `allowed_paths` (what files may be touched).
2. **Default constraints** — Every task gets baseline rules injected at delegation: no out-of-scope changes, no unsolicited refactoring, no signature changes, report issues don't fix them.
3. **Worker prompt** — `buildPrompt()` renders a `--- SCOPE CONSTRAINTS ---` block visible to every worker, combining planner-generated and default constraints.

## Safety Guard

The `safety.Guard` is wired into all adapter constructors and enforced at spawn time:
- `ValidateTaskPaths()` — checks task's allowed_paths against the guard's allowlist
- `CheckCommand()` — scans task description/script for blocked commands
- `IsReadOnly()` — prepends read-only warning to worker prompts when enabled
- All adapter goroutines have `defer/recover` for panic safety

## Task Graph

The task system now includes **cycle detection** via DFS. When `parsePlanOutput()` parses LLM-generated tasks, it validates dependencies with `DetectCycles()` before accepting them. Cycles return an error like `"circular dependency detected: a -> b -> c -> a"`.

## Adapters — Current State

| Adapter | CLI | Non-interactive Command | Status |
|---------|-----|------------------------|--------|
| `kimi` | Kimi Code | `kimi --print --final-message-only -p "<prompt>"` | ✅ **Working, fast (~60s/task)** |
| `opencode` | OpenCode | `opencode run "<prompt>"` | ✅ Working, slow (~2-3min/task) |
| `gemini` | Gemini CLI | `echo "<prompt>" \| gemini` | 🔑 Installed, needs capacity |
| `claude-code` | Claude Code | `claude -p "<prompt>"` | 🔑 Needs `/login` |
| `codex` | Codex | `codex exec "<prompt>"` | 🔑 Needs auth |
| `exec` | bash | `bash -c "<description>"` | ✅ Always available |

## Data Flow

1. **User** runs `queen-bee --adapter kimi run "Review this codebase"`
2. **Queen.plan()** — Spawns one worker to decompose objective into JSON task array (with constraints + allowed_paths per task)
3. **Queen.delegate()** — Assigns ready tasks (respecting deps, cycle-free) to workers up to `MaxParallel`, injects default scope constraints
4. **Queen.monitor()** — Polls workers every 2s, logs every 10s, enforces timeout
5. **Queen.review()** — Collects results; LLM evaluates quality (approved/rejected); handles failures/retries with error classification
6. **Queen.replan()** — After all tasks complete, LLM checks if more work needed
7. Loop back to delegate (new/requeued tasks) or finish
8. **printReport()** — Dumps all task outputs as a final report

## Persistence Layer

```
.hive/
├── hive.db       # SQLite (WAL mode) — primary store
└── log.jsonl     # Legacy append-only event log (parallel write)
```

### SQLite Schema
- **sessions** — one row per `queen-bee run` invocation (id, objective, status, phase, iteration, timestamps)
- **events** — append-only event log indexed by session + type
- **tasks** — full task state (status, worker_id, result JSON, result_data, retries, deps)
- **blackboard** — persisted shared memory (key/value per session)
- **kv** — general purpose key-value store

## CLI Commands

```bash
queen-bee init                          # Create .hive/ and queen.json
queen-bee run "<objective>"              # Run with AI planning
queen-bee --adapter kimi run "<obj>"     # Specify adapter
queen-bee --adapter exec --tasks f.json run "<obj>"  # Pre-defined tasks
queen-bee --workers 8 run "<obj>"        # Set parallelism
queen-bee status                         # Show current/last session
queen-bee config                         # Show configuration
queen-bee resume                         # Resume interrupted session
```

## Configuration (`queen.json`)

```json
{
  "queen": {
    "provider": "kimi",
    "model": "claude-sonnet-4-20250514",
    "max_iterations": 50,
    "plan_timeout": 600000000000,
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
    "kimi": { "command": "kimi", "args": ["--print", "--final-message-only", "-p"] },
    "opencode": { "command": "opencode", "args": ["run"] },
    "gemini": { "command": "gemini" },
    "claude-code": { "command": "claude", "args": ["-p"] },
    "codex": { "command": "codex", "args": ["exec"] },
    "exec": { "command": "bash" }
  },
  "safety": {
    "allowed_paths": ["."],
    "blocked_commands": ["rm -rf /", "sudo rm"],
    "max_file_size": 10485760
  }
}
```

## What Was Tested End-to-End

1. **exec adapter** — parallel shell tasks with dependencies (4 tasks, 2 waves) ✅
2. **opencode adapter** — 15-task code review, 3 waves, 12/15 completed before timeout ✅
3. **kimi adapter** — 5-task codebase review, 2 waves, all completed in ~3 minutes ✅
4. **Pre-defined tasks** (`--tasks file.json`) — loaded and executed with dependency ordering ✅
5. **Retry logic** — failed tasks retried up to `max_retries`, then marked failed ✅
6. **Status command** — SQLite-backed with JSONL fallback for legacy sessions ✅
7. **Real-time output** — worker findings printed as they complete ✅
8. **Final report** — complete consolidated report at end of run ✅
9. **Scope constraints** — kimi workers stayed in `internal/bus/` (1 file) vs. 19 files without constraints ✅
10. **Session status preservation** — Close() preserves 'done'/'failed' status, 3 unit tests ✅
11. **Bus panic recovery** — panicking handlers caught, other handlers still execute, 5 tests ✅
12. **LLM review + replan** — kimi as Queen's brain, approved 4 tasks, replan returned 0 new tasks ✅
13. **Cycle detection** — DFS-based, 12 test cases, integrated into parsePlanOutput() ✅

## Known Test Failures

- `TestErrorClassification/PanicError` — expects retry on panic but queen marks as failed (pre-existing)
- `TestQueenRunWithResumedSession` — resume test has DB session lookup mismatch (pre-existing)

## What Was NOT Built Yet

- No unit tests for config, safety, compact modules
- Context compaction exists but the LLM-backed summarizer is a stub
- No CI/CD, no Makefile, no release process
- The JSONL store is redundant now that SQLite exists
- No OpenAI LLM provider (interface supports it, just needs implementation)

## Dependencies

- `modernc.org/sqlite` — pure Go SQLite driver (no CGO)
- `github.com/anthropics/anthropic-sdk-go` — Anthropic API client
- `github.com/urfave/cli/v3` — CLI framework
- Go 1.26+

## Repository

- GitHub: https://github.com/HexSleeves/queen-bee
- 17 commits on `main`
- No branches, no CI, no releases
