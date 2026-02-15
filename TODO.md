# Waggle — TODO

> Prioritized next steps. Updated 2026-02-15.

## Recently Completed

- [x] **Reduce adapter boilerplate** — Extracted `CLIAdapter` + `CLIWorker` in `generic.go` with 3 prompt modes. Each adapter is now 23-29 lines. 1249 → 471 lines (62% reduction). *(done 2026-02-15)*
- [x] **GitHub Actions CI** — Runs fmt-check + vet + test (with race detector) + build on every push/PR to main. *(done 2026-02-15)*
- [x] **Per-worker timeout with kill** — `Pool.Spawn` wraps context with `WithTimeout` when task has `Timeout > 0`. Process killed on deadline. Bus event published. *(done 2026-02-15)*
- [x] **Cap worker output** — `streamWriter` caps at `workers.max_output_size` (default 1MB). Truncation marker appended. *(done 2026-02-15)*
- [x] **Tests for config, safety, compact** — 46 new tests (1048 lines) covering all three previously untested packages. *(done 2026-02-15)*
- [x] **Queen god-object refactor** — Split `queen.go` into `delegate.go`, `planner.go`, `failure.go`, `reporter.go`. *(upstream 2026-02-15)*
- [x] **Backoff jitter fix** — Was a no-op, now uses actual `rand.Float64()`. *(upstream 2026-02-15)*
- [x] **DFS slice aliasing fix** — Cycle detection path now properly copied to avoid corruption. *(upstream 2026-02-15)*
- [x] **Unchecked DB errors** — `InsertTask`, `UpdateTaskStatus`, `UpdateTaskWorker` returns now checked and logged. *(upstream 2026-02-15)*
- [x] **Goroutine leak in timeout monitor** — Now selects on both parent and child context. *(upstream 2026-02-15)*
- [x] **Unbounded blackboard history** — Capped at 10,000 entries. `Clear()` resets history too. *(upstream 2026-02-15)*
- [x] **Task backoff** — `RetryAfter` field on Task, `Ready()` respects it. *(upstream 2026-02-15)*

## P1 — High (reliability / usability)

- [ ] **Implement real session resume** — CLI loads from SQLite but needs end-to-end testing. Verify interrupted sessions actually resume correctly with task state + conversation history.

- [ ] **TUI resume mode** — Wire TUI into `cmdResume` (currently only plain mode gets TUI).

- [ ] **Adapter health check on startup** — Verify chosen adapter can run a trivial prompt before planning. Fail fast instead of failing on first task.

## P2 — Medium (quality / testing)

- [ ] **Add unit tests for queen orchestrator** — Test Plan→Delegate→Monitor→Review loop with mock Bee. Test agent mode tool dispatch.

- [ ] **Review rejection integration test** — Test that a rejected task actually gets re-queued with feedback and re-executed.

- [ ] **Add `waggle logs` command** — Stream or tail event log for a session from SQLite.

- [ ] **Add `waggle sessions` command** — List all past sessions with objective, status, task counts.

## P3 — Low (polish / extensibility)

- [ ] **Publish binary releases** — GoReleaser or GH Actions workflow for linux/mac/arm64 binaries.

- [ ] **Support mixed adapters per task type** — Allow config like `"code": "kimi", "test": "exec"` so coding tasks use AI while test tasks just run `go test`.

- [ ] **Add `--dry-run` flag** — Run planning only, show task graph, don't execute.

- [ ] **LLM-backed context summarizer** — Replace `compact.DefaultSummarizer` (extractive) with one that calls the Queen's LLM.

- [ ] **Add progress bar / ETA** — Show `[3/5 tasks complete, ~2 min remaining]` in TUI.

- [ ] **Task dependency visualization** — Show DAG structure in TUI or as `dot` graph export.

## Architectural Debt

- The Queen uses the worker adapter for planning in legacy mode (spawns a "planner" worker). Agent mode avoids this.
- The `compact.Context` is wired into the Queen but never read for decision-making. It's write-only.
- The blackboard is both in-memory and persisted (SQLite). On resume, in-memory starts empty.
- `cmdResume` doesn't use the TUI yet (only plain mode).
