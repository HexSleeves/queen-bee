# Waggle — TODO

> Prioritized next steps. Updated 2026-02-16.
>
> Improvements inspired by [Shelley](https://github.com/boldsoftware/shelley) agent architecture.

## What's Done (don't redo these)

- [x] Core orchestration: Plan→Delegate→Monitor→Review loop + Agent mode (tool-using LLM)
- [x] 6 CLI adapters (claude, kimi, codex, opencode, gemini, exec) via shared `CLIAdapter`
- [x] TUI dashboard with Queen/worker panel switching, live streaming output, scroll
- [x] Interactive TUI mode (start without objective, prompt in TUI)
- [x] Per-worker timeout with kill (context.WithTimeout in Pool.Spawn)
- [x] Worker output capped at 1MB (configurable `workers.max_output_size`)
- [x] Parallel task execution (planning prompt + review→delegate shortcut)
- [x] Task retry with jittered exponential backoff (`RetryAfter` field)
- [x] Blackboard history capped at 10k entries
- [x] GitHub Actions CI (fmt-check + vet + test + build)
- [x] Justfile for build/test/run commands
- [x] Queen god-object split into delegate.go, planner.go, failure.go, reporter.go
- [x] Comprehensive test suite: 13,100 lines, 30 test files, all passing
- [x] Session resume E2E — 7 tests covering interrupted session continuity, task state restore, conversation history
- [x] TUI resume mode — `cmdResume` wired into TUI with `runResumeTUI`, shared `startQueenWithFunc` helper
- [x] Adapter health check — `HealthCheck()` on `CLIAdapter`, `setupAdapters()` extracted, fails fast before planning
- [x] `waggle sessions` — list past sessions with task counts, JSON output support
- [x] `waggle logs` — tail/stream event log with `--follow`, emoji icons, JSON output
- [x] Critical bug fixes (PR1) — 8 fixes: multi-word objectives, runJSON panic, runErr race, idempotent Close, assignment cleanup, max-iterations status, ListSessions NULL
- [x] Task synchronization (PR2) — mutex on Task struct, 14 thread-safe getters/setters, all callers updated
- [x] Conversation persistence (PR3) — persist full []ToolMessage, tool-aware compaction, legacy fallback
- [x] Worker pool fixes (PR4) — TOCTOU on capacity check, context cancel leak on spawn failure
- [x] REVIEW.md comprehensive fixes (PR5+PR6) — 20 issues resolved across security, concurrency, cleanup
- [x] Final REVIEW.md items (PR7) — remove DB.Raw(), bus unsubscribe, persist task constraints/context, extract Queen init helper

---

## P0 — Reliability (Agent Loop Hardening)

These prevent crashes and silent data loss in the Queen's agent loop.

### 1. Insert Missing Tool Results (History Repair)

If the agent loop is interrupted after the LLM returns tool_use blocks but before
tool results are appended (crash, cancel, timeout), the conversation history becomes
corrupted. The next LLM call fails with "tool_use ids were found without tool_result
blocks". Shelley's `insertMissingToolResults` fixes this before every LLM call.

**Files to change:**
- `internal/queen/agent.go` — add `repairToolHistory(messages []llm.ToolMessage) []llm.ToolMessage`

**Implementation:**
- Scan messages looking for assistant messages with tool_use content blocks
- For each, check that the next message is a tool_result with matching IDs
- If missing: insert a synthetic tool_result message with `is_error: true` and
  content `"not executed; retry possible"`
- Remove orphan tool_results whose IDs don't match the preceding assistant's tool_use blocks
- Handle empty assistant messages (add placeholder text content)
- Call `repairToolHistory()` before every `ChatWithTools()` call in both `RunAgent()` and `RunAgentResume()`

**Tests:**
- `internal/queen/agent_test.go`:
  - `TestRepairToolHistory_MissingResults` — assistant has tool_use, next message is user text not tool_result
  - `TestRepairToolHistory_OrphanResults` — tool_result references non-existent tool_use ID
  - `TestRepairToolHistory_EmptyAssistant` — empty content gets placeholder
  - `TestRepairToolHistory_NoOpWhenValid` — valid history passes through unchanged
  - `TestRepairToolHistory_ResumedSession` — simulates resume from crash mid-tool-execution

**Estimated size:** ~120 lines code + ~100 lines tests

---

### 2. Handle `max_tokens` Truncation

When the LLM response is truncated (StopReason == "max_tokens"), tool_use blocks
may be incomplete/lost. Currently Waggle ignores this — it treats "max_tokens" like
"end_turn" and silently drops the turn. Shelley records the truncated response
(excluded from context) and injects an error message telling the LLM to retry smaller.

**Files to change:**
- `internal/queen/agent.go` — add handling after `ChatWithTools()` returns, before tool execution

**Implementation:**
- After receiving response, check `resp.StopReason == "max_tokens"`
- If truncated:
  - Do NOT append the truncated assistant message to conversation history
  - Log a warning: `"⚠ LLM response truncated (max_tokens), requesting retry"`
  - Append a synthetic user message:
    ```
    [SYSTEM: Your previous response was truncated because it exceeded the maximum
    output token limit. Any tool calls in that response were lost. Please retry
    with fewer tool calls or shorter text output.]
    ```
  - Continue the loop (don't count as a "wasted" turn — but do count toward maxTurns)
- Apply to both `RunAgent()` and `RunAgentResume()` loops

**Tests:**
- `internal/queen/agent_test.go`:
  - `TestMaxTokensTruncation_RetriesSuccessfully` — mock LLM returns max_tokens then end_turn
  - `TestMaxTokensTruncation_MessageNotInHistory` — verify truncated content excluded

**Estimated size:** ~30 lines code + ~50 lines tests

---

### 3. Proper Retry with Error Classification

Replace the naive "sleep 2s, retry once" with classified retries and backoff.
Currently if the first retry fails, the entire session dies.

**Files to change:**
- `internal/llm/retry.go` (new file) — `IsRetryable(error) bool` and `RetryLLMCall()` helper
- `internal/queen/agent.go` — replace inline retry with `RetryLLMCall()`

**Implementation:**
- `IsRetryable(err error) bool` — check for:
  - `io.EOF`, `io.ErrUnexpectedEOF`
  - "connection reset", "connection refused", "i/o timeout", "no such host"
  - HTTP 429 (rate limit), 500, 502, 503, 529 (overloaded)
  - Anthropic `overloaded_error` type
- `RetryLLMCall(ctx, maxRetries int, fn func() (*Response, error)) (*Response, error)`:
  - Up to `maxRetries` attempts (default 3)
  - Backoff: 1s, 2s, 4s (exponential, capped)
  - Only retry if `IsRetryable()` returns true
  - Log each retry attempt with error and attempt number
- Replace both retry blocks in `RunAgent()` and `RunAgentResume()`

**Tests:**
- `internal/llm/retry_test.go`:
  - `TestIsRetryable_EOF` / `TestIsRetryable_ConnectionReset` / `TestIsRetryable_RateLimit`
  - `TestIsRetryable_PermanentError` — non-retryable errors return false
  - `TestRetryLLMCall_SucceedsAfterRetry` — fails twice, succeeds third
  - `TestRetryLLMCall_GivesUpAfterMax` — fails all attempts, returns last error
  - `TestRetryLLMCall_NoRetryOnPermanent` — returns immediately on non-retryable

**Estimated size:** ~80 lines code + ~100 lines tests

---

## P1 — Cost & Performance

### 4. Prompt Caching for Anthropic

Anthropic's prompt caching reduces input token costs by ~90% for repeated prefixes.
Shelley sets cache breakpoints on the last tool definition and last user message.
Waggle sends bare requests with no caching hints.

**Files to change:**
- `internal/llm/types.go` — add `Cache bool` field to `ToolDef` and `ContentBlock`
- `internal/llm/anthropic.go` — honor `Cache` field when building API params
- `internal/queen/agent.go` — set cache flags before calling `ChatWithTools()`

**Implementation:**
- Add `Cache bool` to `ToolDef` and `ContentBlock` structs (json tag `"cache,omitempty"`)
- In `AnthropicClient.ChatWithTools()`:
  - When building `apiTools`, if `td.Cache` is true, set `CacheControl` on the tool param:
    `anthropic.CacheControlEphemeralParam{Type: "ephemeral"}`
  - When building system blocks, set cache on last system block
- In `agent.go`, before calling `ChatWithTools()`:
  - Make a shallow copy of tools slice, set `Cache: true` on last tool
  - Find the last user message, make a shallow copy, set `Cache: true` on last content block
- OpenAI and Gemini clients ignore the `Cache` field (no-op)

**Tests:**
- `internal/llm/anthropic_test.go`:
  - `TestAnthropicCacheFlags` — verify cache control appears in serialized request
- `internal/queen/agent_test.go`:
  - `TestPromptCaching_SetOnLastToolAndUser` — verify flags set correctly

**Estimated size:** ~40 lines code + ~40 lines tests

---

### 5. LLM Usage Tracking

Waggle has no visibility into token consumption. Shelley tracks input/output tokens,
cache hits/misses, model name, and timing per response.

**Files to change:**
- `internal/llm/types.go` — add `Usage` struct and embed in `Response`
- `internal/llm/anthropic.go` — extract usage from Anthropic response
- `internal/llm/openai.go` — extract usage from OpenAI response
- `internal/llm/gemini.go` — extract usage from Gemini response
- `internal/queen/agent.go` — accumulate and log usage
- `internal/queen/reporter.go` — print usage summary in final report

**Implementation:**
- New `Usage` struct:
  ```go
  type Usage struct {
      InputTokens        int `json:"input_tokens"`
      OutputTokens       int `json:"output_tokens"`
      CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
      CacheReadTokens    int `json:"cache_read_tokens,omitempty"`
  }
  ```
- Add `Usage Usage` and `Model string` fields to `Response`
- In each provider's response parsing, extract usage from the API response:
  - Anthropic: `resp.Usage.InputTokens`, `resp.Usage.OutputTokens`, `resp.Usage.CacheCreationInputTokens`, `resp.Usage.CacheReadInputTokens`
  - OpenAI: `resp.Usage.PromptTokens`, `resp.Usage.CompletionTokens`
  - Gemini: `resp.UsageMetadata.PromptTokenCount`, `resp.UsageMetadata.CandidatesTokenCount`
- In agent loop: accumulate `totalUsage Usage` across turns, log per-turn usage
- In reporter: print `"Token usage: Xk input, Yk output (Zk cached)"`
- Store per-session usage in DB (add columns to sessions table or use kv)

**Tests:**
- `internal/llm/anthropic_test.go`: `TestAnthropicUsageParsing`
- `internal/llm/openai_test.go`: `TestOpenAIUsageParsing`
- `internal/queen/agent_test.go`: `TestUsageAccumulation`

**Estimated size:** ~100 lines code + ~60 lines tests

---

### 6. Tool Execution Timing

Track how long each Queen tool call takes. Useful for identifying slow workers
and tuning timeouts.

**Files to change:**
- `internal/queen/agent.go` — record start/end time around `executeTool()`
- `internal/queen/reporter.go` — include tool timing in final report
- `internal/bus/bus.go` — (optional) emit timing events

**Implementation:**
- Record `startTime := time.Now()` before `executeTool()` and `duration := time.Since(startTime)` after
- Log duration: `"🔧 Tool: assign_task (2.3s)"`
- Accumulate per-tool-name timing map: `map[string][]time.Duration`
- In reporter: print `"Tool timing: assign_task avg 1.2s (5 calls), wait_for_workers avg 45s (3 calls)"`
- Emit bus event `tool.executed` with name, duration, success/error

**Tests:**
- `internal/queen/agent_test.go`: `TestToolTimingLogged` — verify timing map populated

**Estimated size:** ~40 lines code + ~30 lines tests

---

## P2 — Robustness

### 7. Conversation History Compaction Improvements

Current compaction is crude: extractive text summary, no LLM involvement.
Also, the cut-point finder has a fallback to `idealCut` that could split
tool_use/tool_result pairs.

**Files to change:**
- `internal/queen/agent.go` — fix compaction edge cases
- `internal/compact/compact.go` — add LLM-backed summarizer option

**Implementation:**
- **Fix pair splitting bug**: When fallback to `idealCut` triggers, scan forward from
  idealCut to find a safe boundary instead of using it raw. Add assertion that the
  first kept message is never role=tool_result.
- **Add LLM summarizer**: `LLMSummarizer(client llm.Client) func(messages []Message) (string, error)`
  - Sends compacted messages to LLM with prompt: "Summarize the key decisions, tool
    calls, and results from this conversation segment. Be concise."
  - Falls back to `DefaultSummarizer` if LLM call fails
- **Count tokens in compacted summary**: Ensure summary + remaining messages fit
  within a reasonable budget (e.g., summary should be <2000 tokens)
- Wire `LLMSummarizer` into agent.go's `compactMessages()` when `q.llm` is available

**Tests:**
- `internal/queen/agent_test.go`:
  - `TestCompactMessages_NeverSplitsPairs` — generate pathological message sequence
  - `TestCompactMessages_FallbackSafe` — verify fallback doesn't corrupt
- `internal/compact/compact_test.go`:
  - `TestLLMSummarizer` — mock LLM returns summary
  - `TestLLMSummarizer_Fallback` — LLM fails, falls back to default

**Estimated size:** ~60 lines code + ~80 lines tests

---

### 8. Large Worker Output Handling

Workers producing huge output (e.g., verbose test runs) can bloat the conversation
context when the Queen reads it via `get_task_output`. Shelley's bash tool saves
oversized output to a file and returns head+tail with a path.

**Files to change:**
- `internal/queen/tools.go` — `handleGetTaskOutput()` truncation logic
- `internal/worker/worker.go` — (already capped at 1MB, good)

**Implementation:**
- In `handleGetTaskOutput()`, if output exceeds a threshold (e.g., 8KB):
  - Save full output to `.hive/outputs/<task_id>.log`
  - Return to the Queen: first 3KB + `"\n\n[...truncated, {N} bytes total...]\n\n"` + last 3KB
  - Include message: `"Full output saved to .hive/outputs/<task_id>.log — use read_file to inspect specific sections."`
- Keep the full output available via `read_file` tool (already exists)

**Tests:**
- `internal/queen/tools_test.go`:
  - `TestGetTaskOutput_SmallOutput` — returned verbatim
  - `TestGetTaskOutput_LargeOutput` — truncated with head+tail
  - `TestGetTaskOutput_FileWritten` — verify file exists in .hive/outputs/

**Estimated size:** ~50 lines code + ~60 lines tests

---

### 9. Git State Change Tracking

After workers complete tasks, the Queen has no awareness of what git changes
were made. Shelley tracks branch, commit, worktree status at end of each turn.

**Files to change:**
- `internal/queen/gitstate.go` (new file) — `GitState` struct and diff detection
- `internal/queen/agent.go` — check git state after worker completion
- `internal/queen/tools.go` — include git changes in `wait_for_workers` results

**Implementation:**
- `GitState` struct: `Branch string`, `Commit string`, `DirtyFiles int`, `UntrackedFiles int`
- `GetGitState(projectDir string) *GitState` — runs `git rev-parse`, `git status --porcelain`
- `(g *GitState) Equal(other *GitState) bool`
- `(g *GitState) Diff(other *GitState) string` — human-readable summary of changes
- In `handleWaitForWorkers()`: capture git state before and after, include diff in result
  message: `"Git: 2 new commits on main, 3 files changed"`
- Emit bus event `git.state_changed` for TUI

**Tests:**
- `internal/queen/gitstate_test.go`:
  - `TestGetGitState` — uses `t.TempDir()` with `git init`
  - `TestGitStateDiff` — commit changes, verify diff description
  - `TestGitStateEqual` — no changes returns true

**Estimated size:** ~80 lines code + ~60 lines tests

---

## P3 — UX & Observability

### 10. Dual-Content Model for Tools

Currently all Queen tools return a single `string` used for both LLM context and
TUI display. This means the TUI shows raw JSON/text meant for the LLM. A dual-content
model separates what the model sees from what the user sees.

**Files to change:**
- `internal/queen/tools.go` — change `ToolHandler` return type
- `internal/queen/agent.go` — handle dual content in tool execution
- `internal/tui/events.go` — new message types for structured tool display
- `internal/tui/view.go` — render display content differently

**Implementation:**
- New `ToolOutput` struct:
  ```go
  type ToolOutput struct {
      LLMContent string      // What gets sent to the LLM
      Display    interface{} // What gets shown in TUI (optional, falls back to LLMContent)
  }
  ```
- Change `ToolHandler` signature:
  `func(ctx context.Context, q *Queen, input json.RawMessage) (ToolOutput, error)`
- Update all 11 tool handlers to return `ToolOutput`. Initially, `Display` can be nil
  (uses LLMContent). Then incrementally add structured display for key tools:
  - `get_status` → display as a formatted table
  - `assign_task` → display task title and worker ID
  - `wait_for_workers` → display as completion summary with durations
  - `create_tasks` → display as task list with dependency arrows
- Emit `ToolDisplayMsg` to TUI with the structured display content
- TUI renders structured content with colors and formatting vs. raw text

**Tests:**
- `internal/queen/tools_test.go`: verify all handlers return valid `ToolOutput`
- `internal/tui/view_test.go`: verify display rendering (if testable)

**Estimated size:** ~150 lines code + ~50 lines tests (refactor-heavy)

---

### 11. Per-Message DB Storage (Replace JSON Blob)

Currently the entire conversation is serialized as a single JSON blob in the KV table.
This prevents per-message querying, cost tracking per turn, and selective exclusion.

**Files to change:**
- `internal/state/db.go` — new `messages` table and methods
- `internal/queen/agent.go` — replace `persistConversation()`/`loadConversation()` with per-message ops

**Implementation:**
- New `messages` table:
  ```sql
  CREATE TABLE IF NOT EXISTS messages (
      id          INTEGER PRIMARY KEY AUTOINCREMENT,
      session_id  TEXT NOT NULL,
      sequence_id INTEGER NOT NULL,
      role        TEXT NOT NULL,
      content     TEXT NOT NULL,  -- JSON
      usage_data  TEXT,           -- JSON (tokens, model, timing)
      excluded    INTEGER DEFAULT 0,
      created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
      UNIQUE(session_id, sequence_id)
  );
  ```
- `DB.AppendMessage(ctx, sessionID, msg ToolMessage, usage *Usage) error`
- `DB.LoadMessages(ctx, sessionID) ([]ToolMessage, error)`
- `DB.MarkExcluded(ctx, sessionID, sequenceID) error`
- Migrate: keep KV-based persistence as fallback for existing sessions
- In agent loop: call `AppendMessage()` after each assistant response and tool_result
- On resume: `LoadMessages()` instead of loading JSON blob

**Tests:**
- `internal/state/db_test.go`:
  - `TestAppendAndLoadMessages` — round-trip
  - `TestMarkExcluded` — excluded messages omitted from load
  - `TestMessageOrdering` — sequence_id preserved

**Estimated size:** ~100 lines code + ~80 lines tests

---

### 12. SQLite Read/Write Pool Separation

Under load (TUI polling status while Queen writes), the single connection pool
causes contention. Shelley uses 1 writer + 3 readers with `query_only` pragma.

**Files to change:**
- `internal/state/db.go` — split into writer and reader connections

**Implementation:**
- Open two `*sql.DB` instances: `writer` (max 1 conn) and `reader` (max 3 conns)
- Reader connections: `PRAGMA query_only = 1`
- Route all `SELECT`-only methods through reader, all writes through writer
- Keep WAL mode and busy timeout on both
- Graceful close: close both pools

**Tests:**
- `internal/state/db_test.go`:
  - `TestConcurrentReadWrite` — writer inserts while reader queries simultaneously
  - `TestReaderCannotWrite` — verify reader rejects INSERT/UPDATE

**Estimated size:** ~60 lines code + ~40 lines tests

---

## P3 — Low Priority

- [ ] **Binary releases** — GoReleaser or GH Actions for linux/mac/arm64
- [ ] **Mixed adapters per task type** — e.g., `"code": "kimi", "test": "exec"`
- [ ] **`--dry-run` flag** — Show planned task graph without executing
- [ ] **Progress bar / ETA in TUI** — `[3/5 tasks, ~2 min remaining]`
- [ ] **Task dependency DAG visualization** — TUI or `dot` export
- [ ] **Review Rejection Integration Test** — test reject→re-queue→re-execute cycle

## Architectural Debt

- Legacy mode uses the worker adapter for planning (spawns a "planner" worker). Agent mode avoids this.
- Blackboard is in-memory + persisted. On resume, in-memory starts empty.

## VM Notes

- **Disk space**: ~19GB total, can fill up. Run `go clean -cache` to reclaim ~1GB.
- **Auth**: kimi is rate-limited, claude-code needs `/login`, no API keys set for Anthropic/OpenAI/Gemini.
- **exec adapter always works** for testing.
- **PAT**: Needs `workflow` scope to push `.github/workflows/` changes.
