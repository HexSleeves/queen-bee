# Waggle Code Review & Optimization Analysis

**Date:** 2026-02-14
**Scope:** Full repository (`~15k LOC Go, 50 files, 13 packages`)
**Baseline:** `go vet` clean, all tests pass (14 packages)

---

## Table of Contents

- [Summary](#summary)
- [Critical Issues](#critical-issues)
- [High Issues](#high-issues)
- [Medium Issues](#medium-issues)
- [Low Issues](#low-issues)
- [Positive Observations](#positive-observations)
- [Recommended Fix Order](#recommended-fix-order)

---

## Summary

| Severity | Count | Key Theme |
|----------|-------|-----------|
| **CRITICAL** | 4 | Backoff no-op, agent mode adapter, missing retry in agent mode, router skips availability |
| **HIGH** | 5 | Memory leak (assignments), DFS aliasing, timer leak, goroutine leak, unbounded history |
| **MEDIUM** | 6 | God object, DRY violations, guard re-creation, logger bypass, clear/history mismatch |
| **LOW** | 8 | Typing, perf micro-opts, test coverage gaps, UTF-8 |

---

## Critical Issues

### 1. Backoff goroutine is a no-op

**Location:** `internal/queen/queen.go:818-822`

**Current code:**

```go
// Apply backoff by waiting before the task becomes ready again
go func() {
    time.Sleep(backoffDelay)
}()
```

**Issue:** The goroutine sleeps and dies without affecting anything. The task is already set back to `StatusPending` on line 813, so the next iteration picks it up immediately. Exponential backoff is completely broken.

**Recommendation:** Add a `RetryAfter time.Time` field to `task.Task`. In `handleTaskFailure`:

```go
t.RetryAfter = time.Now().Add(backoffDelay)
```

In `delegate()` and `TaskGraph.Ready()`, skip tasks where `time.Now().Before(t.RetryAfter)`.

**Impact:** Correctness — retryable failures hammer the worker immediately instead of backing off. Rate-limited APIs will cascade-fail.

**Priority:** Critical

---

### 2. Agent mode: no adapter fallback

**Location:** `internal/queen/agent.go:40-45`

**Current code:**

```go
defaultAdapter := q.cfg.Workers.DefaultAdapter
if a, ok := q.registry.Get(defaultAdapter); !ok || !a.Available() {
    if !q.quiet {
        q.logger.Printf("⚠ Default adapter %q not available, falling back to: %s", defaultAdapter, available[0])
    }
}
```

**Issue:** Logs the warning but never updates the router. Compare to `queen.go:326-336` which correctly calls `q.router.SetRoute(tt, available[0])` for all task types. In agent mode, `assign_task` and all other tools still use the unavailable default adapter, causing every task spawn to fail.

**Recommendation:** Copy the same `SetRoute` loop from `Run()`:

```go
if a, ok := q.registry.Get(defaultAdapter); !ok || !a.Available() {
    allTypes := []task.Type{task.TypeCode, task.TypeResearch, task.TypeTest, task.TypeReview, task.TypeGeneric}
    for _, tt := range allTypes {
        q.router.SetRoute(tt, available[0])
    }
    if !q.quiet {
        q.logger.Printf("⚠ Default adapter %q not available, falling back to: %s", defaultAdapter, available[0])
    }
}
```

**Impact:** Correctness — agent mode is broken when the configured default adapter isn't installed.

**Priority:** Critical

---

### 3. `TaskRouter.Route` doesn't check `Available()`

**Location:** `internal/adapter/adapter.go` (Route method)

**Current code:**

```go
func (tr *TaskRouter) Route(t *task.Task) string {
    if name, ok := tr.routes[t.Type]; ok {
        return name
    }
    return tr.defaultAdapter
}
```

**Issue:** Returns adapter names without verifying the adapter is actually installed/available. `Pool.Spawn` then creates a worker that immediately fails when the binary isn't found.

**Recommendation:**

```go
func (tr *TaskRouter) Route(t *task.Task) string {
    if name, ok := tr.routes[t.Type]; ok {
        if a, ok := tr.registry.Get(name); ok && a.Available() {
            return name
        }
    }
    if a, ok := tr.registry.Get(tr.defaultAdapter); ok && a.Available() {
        return tr.defaultAdapter
    }
    return ""
}
```

**Impact:** Correctness — tasks fail at spawn time instead of being routed to an available adapter.

**Priority:** Critical

---

### 4. Agent-mode `processWorkerResults` skips retry logic

**Location:** `internal/queen/tools.go:649-656`

**Current code:**

```go
} else {
    q.tasks.UpdateStatus(taskID, task.StatusFailed)
    t, _ := q.tasks.Get(taskID)
    if t != nil {
        t.Result = result
    }
    q.db.UpdateTaskStatus(ctx, q.sessionID, taskID, "failed")
}
```

**Issue:** Failed tasks go straight to `StatusFailed` with no error classification, no retry, no backoff. The legacy `review()` path handles this properly via `handleTaskFailure()`, but agent mode doesn't call it.

Missing behaviors:

- Error classification (`errors.ClassifyError`)
- Retry logic (exponential backoff, `MaxRetries`)
- `UpdateTaskErrorType` DB call
- Distinction between retryable and permanent errors

**Recommendation:** Call `q.handleTaskFailure(ctx, taskID, workerID, result)` in the `else` branch, or extract shared failure-handling logic into a function used by both code paths.

**Impact:** Correctness — agent mode permanently fails tasks that should be retried.

**Priority:** Critical

---

## High Issues

### 5. `assignments` map leaks memory

**Location:** `internal/queen/queen.go:66`

**Current code:**

```go
assignments  map[string]string // workerID -> taskID
```

**Issue:** Entries are added in `delegate()` (line 582) and `handleAssignTask()` (line 356) but never removed. `pool.Cleanup()` removes bees from the pool, but the queen's `assignments` map keeps growing for the entire session. Long-running sessions or sessions with many tasks will leak memory proportional to total workers spawned.

**Recommendation:** After processing a finished worker in `review()`, delete from `assignments`:

```go
q.mu.Lock()
delete(q.assignments, workerID)
q.mu.Unlock()
```

Or have `pool.Cleanup()` return removed IDs so the Queen can prune `assignments`.

**Impact:** Memory leak in long-running sessions.

**Priority:** High

---

### 6. DFS cycle detection: slice aliasing bug

**Location:** `internal/task/task.go:222-255`

**Current code:**

```go
func (g *TaskGraph) detectCycleDFS(nodeID string, visited, recStack map[string]bool, path []string) []string {
    visited[nodeID] = true
    recStack[nodeID] = true
    path = append(path, nodeID)
```

**Issue:** `append` may reuse the backing array. When iterating over multiple dependents (line 233), the second child's recursion can write into the same backing array that the parent's `path` still references. This corrupts the cycle path reported by earlier iterations.

**Recommendation:** Copy the path per recursive call:

```go
newPath := make([]string, len(path), len(path)+1)
copy(newPath, path)
newPath = append(newPath, nodeID)
```

Or pass `path` immutably by always creating a fresh slice before recursion.

**Impact:** Correctness — cycle detection can report wrong cycle paths.

**Priority:** High

---

### 7. `time.After` leak in `handleWaitForWorkers`

**Location:** `internal/queen/tools.go:568`

**Current code:**

```go
deadline := time.After(time.Duration(timeoutSec) * time.Second)
```

**Issue:** `time.After` creates a timer that runs until it fires, even if the function returns early (workers complete). With the default 300s timeout, each early return leaks a goroutine + timer for up to 5 minutes.

**Recommendation:** Use `time.NewTimer` + `defer timer.Stop()`:

```go
timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
defer timer.Stop()
// ...
case <-timer.C:
```

**Impact:** Resource leak — goroutine + timer held for up to 5 minutes per early return.

**Priority:** High

---

### 8. Goroutine can outlive parent context

**Location:** `internal/worker/worker.go:107-121`

**Current code:**

```go
go func() {
    <-spawnCtx.Done()
    cancel()
    if spawnCtx.Err() == context.DeadlineExceeded {
        if p.msgBus != nil {
            p.msgBus.Publish(bus.Message{
                Type:     bus.MsgWorkerFailed,
                WorkerID: workerID,
                TaskID:   t.ID,
                Payload:  fmt.Sprintf("timed out after %s", t.Timeout),
                Time:     time.Now(),
            })
        }
    }
}()
```

**Issue:** This goroutine blocks on `spawnCtx.Done()` only. If the parent `ctx` is cancelled (e.g., SIGINT), `spawnCtx` won't be done until its own timeout fires. The goroutine is held alive longer than needed.

**Recommendation:** Select on both contexts:

```go
go func() {
    select {
    case <-ctx.Done():
        cancel()
    case <-spawnCtx.Done():
        cancel()
        if spawnCtx.Err() == context.DeadlineExceeded {
            // publish timeout event
        }
    }
}()
```

**Impact:** Goroutine leak on graceful shutdown.

**Priority:** High

---

### 9. Blackboard history unbounded

**Location:** `internal/blackboard/blackboard.go:42`

**Current code:**

```go
bb.history = append(bb.history, entry)
```

**Issue:** No cap on history size. Long sessions with frequent blackboard updates (every task posts results) will consume unbounded memory.

**Recommendation:** Add a `maxHistory` field (like `bus.MessageBus` does with `maxHist`) and trim when exceeded:

```go
bb.history = append(bb.history, entry)
if len(bb.history) > bb.maxHistory {
    bb.history = bb.history[len(bb.history)-bb.maxHistory:]
}
```

**Impact:** Memory growth proportional to session length.

**Priority:** High

---

## Medium Issues

### 10. God object: `queen.go` at 1,280 lines

**Location:** `internal/queen/queen.go`

**Issue:** Queen handles planning, delegation, monitoring, review, failure handling, DB persistence, LLM calls, blackboard, phase transitions, prompt building, JSON parsing, and report printing. This violates SRP and makes the file difficult to navigate and test.

**Recommendation:** Split into focused files:

| New File | Responsibility | Functions to Move |
|----------|---------------|-------------------|
| `planner.go` | Task decomposition | `plan()`, `buildPlanPrompt()`, `parsePlanOutput()`, JSON helpers |
| `delegator.go` | Task assignment | `delegate()`, `injectDefaultConstraints()` |
| `monitor.go` | Worker monitoring | `monitor()`, `waitForWorker()` |
| `failure.go` | Error handling | `handleTaskFailure()`, `handleFailure()` |
| `reporter.go` | Output | `printReport()`, `buildSummary()`, `Results()` |
| `queen.go` | Core struct + `Run()` loop + phase transitions | Keep small |

**Impact:** Maintainability, testability.

**Priority:** Medium

---

### 11. Duplicate scope constraint injection

**Location:** `internal/queen/queen.go:568-573` and `internal/queen/tools.go:343-348`

**Issue:** Same 4-line constraint block copy-pasted in `delegate()` and `handleAssignTask()`. Changes to constraints must be made in both places.

**Recommendation:** Extract to a method:

```go
func injectDefaultConstraints(t *task.Task) {
    t.Constraints = appendUnique(t.Constraints,
        "Do NOT make changes outside the scope described in this task",
        "Do NOT refactor, reorganize, or 'improve' code unrelated to this task",
        "Do NOT modify function signatures unless explicitly asked to",
        "If you find issues outside your scope, note them in your output but do NOT fix them",
    )
}
```

**Impact:** DRY violation, risk of divergence.

**Priority:** Medium

---

### 12. Duplicate DB insert pattern across 5+ locations

**Locations:**

- `internal/queen/queen.go:450-454` (plan, predefined tasks)
- `internal/queen/queen.go:477-481` (plan, exec adapter)
- `internal/queen/queen.go:527-532` (plan, AI decomposed)
- `internal/queen/queen.go:744-749` (review, replan)
- `internal/queen/tools.go:279-289` (create_tasks)

**Issue:** Task creation and DB persistence are repeated in all these locations. The `state.TaskRow` construction is verbose and any schema change requires updating 5+ places.

**Recommendation:** Introduce a helper:

```go
func (q *Queen) persistNewTask(ctx context.Context, t *task.Task) {
    q.tasks.Add(t)
    q.db.InsertTask(ctx, q.sessionID, state.TaskRow{
        ID: t.ID, Type: string(t.Type), Status: string(t.Status),
        Priority: int(t.Priority), Title: t.Title, Description: t.Description,
        MaxRetries: t.MaxRetries, DependsOn: strings.Join(t.DependsOn, ","),
    })
}
```

**Impact:** DRY violation, maintenance burden.

**Priority:** Medium

---

### 13. `safety.Guard` recreated on every tool call

**Locations:** `internal/queen/tools.go:677-679` (handleReadFile), `internal/queen/tools.go:746-748` (handleListFiles)

**Current code:**

```go
guard, err := safety.NewGuard(q.cfg.Safety, q.cfg.ProjectDir)
if err != nil {
    return "", fmt.Errorf("init safety guard: %w", err)
}
```

**Issue:** A new `safety.Guard` is created on every `read_file` and `list_files` call. The guard is already available on every adapter (passed during `New()` in queen.go). This is wasteful and could diverge from the adapter's guard configuration.

**Recommendation:** Store the guard on the Queen struct or expose it from the registry.

**Impact:** Unnecessary allocations, potential configuration drift.

**Priority:** Medium

---

### 14. `KillAll` uses `fmt.Printf` instead of logger

**Location:** `internal/worker/worker.go:164-165`

**Current code:**

```go
if err != nil {
    fmt.Printf("error killing worker %s: %v\n", w.ID(), err)
}
```

**Issue:** Writes directly to stdout, bypassing the logger. Breaks TUI mode (output mixes with Bubbletea rendering), JSON mode (non-JSON output), and quiet mode (shouldn't show anything).

**Recommendation:** Accept a logger parameter or return errors:

```go
func (p *Pool) KillAll() []error {
    // ...
    errs = append(errs, fmt.Errorf("kill %s: %w", w.ID(), err))
}
```

**Impact:** Output corruption in TUI/JSON modes.

**Priority:** Medium

---

### 15. `Clear()` doesn't reset history

**Location:** `internal/blackboard/blackboard.go:140-143`

**Current code:**

```go
func (bb *Blackboard) Clear() {
    bb.mu.Lock()
    defer bb.mu.Unlock()
    bb.entries = make(map[string]*Entry)
}
```

**Issue:** `History()` still returns old entries after `Clear()`. Semantic violation — callers expect `Clear()` to fully reset the blackboard.

**Recommendation:** Add `bb.history = bb.history[:0]` in `Clear()`.

**Impact:** Correctness — stale history after clear.

**Priority:** Medium

---

## Low Issues

### 16. `interface{}` in `Blackboard.Entry.Value`

**Location:** `internal/blackboard/blackboard.go:12`

```go
Value interface{} `json:"value"`
```

**Issue:** Always used as `string` throughout the codebase (e.g., `tools.go:449` does `s, ok := entry.Value.(string)`). The `interface{}` type adds unnecessary runtime assertions and loses type safety.

**Recommendation:** Change to `string`.

**Impact:** Readability, type safety.

**Priority:** Low

---

### 17. String concatenation in `Summarize()`

**Location:** `internal/blackboard/blackboard.go:153-160`

```go
summary := "Blackboard contents:\n"
for k, e := range bb.entries {
    summary += "- [" + k + "] by " + e.PostedBy
```

**Issue:** O(n²) string concatenation in a loop. Should use `strings.Builder`.

**Impact:** Performance for large blackboards.

**Priority:** Low

---

### 18. Bus history trim doesn't release memory

**Location:** `internal/bus/bus.go:67-69`

```go
if len(b.history) > b.maxHist {
    b.history = b.history[len(b.history)-b.maxHist:]
}
```

**Issue:** Re-slicing keeps the original backing array allocated. Over time, the underlying array grows without bound even though only `maxHist` elements are visible.

**Recommendation:** Copy to a new slice when trimming:

```go
if len(b.history) > b.maxHist {
    trimmed := make([]Message, b.maxHist)
    copy(trimmed, b.history[len(b.history)-b.maxHist:])
    b.history = trimmed
}
```

Or use a ring buffer.

**Impact:** Memory — backing array never shrinks.

**Priority:** Low

---

### 19. `truncate()` operates on bytes, not runes

**Location:** `internal/queen/queen.go:1274-1279`

```go
func truncate(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "..."
}
```

**Issue:** Can split multi-byte UTF-8 characters, producing invalid strings. LLM outputs often contain Unicode.

**Recommendation:**

```go
func truncate(s string, max int) string {
    runes := []rune(s)
    if len(runes) <= max {
        return s
    }
    return string(runes[:max]) + "..."
}
```

**Impact:** Correctness — invalid UTF-8 in output.

**Priority:** Low

---

### 20. `CalculateBackoffWithJitter` has no randomness

**Location:** `internal/errors/errors.go:278-293`

```go
jitter := time.Duration(float64(delay) * jitterPercent)
if jitter > 0 {
    delay -= jitter / 2 // Reduce by up to half of jitter range
}
```

**Issue:** Always subtracts a fixed amount. The function claims jitter but produces deterministic results. Multiple workers retrying the same error will still thundering-herd.

**Recommendation:** Either use `math/rand` for actual randomness or remove the function and document `CalculateBackoff` as jitter-free.

**Impact:** Correctness — jitter claim is misleading.

**Priority:** Low

---

### 21. Missing test coverage for critical packages

| Package | Has Tests | Risk |
|---------|-----------|------|
| `internal/compact` | No | Low — simple context management |
| `internal/config` | No | Medium — config parsing errors surface at runtime |
| `internal/output` | No | Low — display logic |
| `internal/safety` | **No** | **High — this is the security boundary** |
| `internal/tui` | No | Low — UI rendering |
| `cmd/waggle` | No | Low — thin CLI wrapper |

**Recommendation:** Prioritize `safety` and `config` test coverage. Safety is the guard preventing arbitrary file access and command execution.

**Impact:** Confidence in security boundary.

**Priority:** Low (but safety tests are medium priority)

---

### 22. `jsonInt` uses `fmt.Sscanf` for string→int

**Location:** `internal/queen/queen.go:1247-1249`

```go
var n int
fmt.Sscanf(val, "%d", &n)
return n
```

**Issue:** `fmt.Sscanf` is ~10x slower than `strconv.Atoi` and silently swallows errors.

**Recommendation:**

```go
n, _ := strconv.Atoi(val)
return n
```

**Impact:** Minor performance in JSON parsing.

**Priority:** Low

---

### 23. Struct padding in `task.Task`

**Location:** `internal/task/task.go:42-64`

**Issue:** Fields are ordered for readability, not memory layout. Grouping pointer-sized fields together would save ~8-16 bytes per Task struct due to alignment padding.

**Impact:** Minor memory savings, only matters at high task counts.

**Priority:** Low

---

## Positive Observations

- **Mutex usage is generally correct** — `RWMutex` for read-heavy paths, `Mutex` for write paths, consistent lock ordering
- **Bus handlers wrapped with panic recovery** — prevents a bad handler from crashing the orchestrator
- **DB uses `context.Context` and parameterized queries** — no SQL injection risk
- **Cycle detection exists** in the task graph (despite the aliasing bug, the logic is sound)
- **Error classification** distinguishes retryable vs permanent errors with both pattern matching and exit codes
- **Task validation** in tools checks required fields and duplicate IDs
- **`defer tx.Rollback()`** used correctly in DB transactions
- **Clean separation** between adapter interface and implementations
- **Good use of `strings.Builder`** in most prompt construction (except `Summarize`)

---

## Recommended Fix Order

### Priority 1 — Ship Blockers (fix now)

1. **#1** — Implement real backoff with `RetryAfter` field
2. **#2** — Add adapter fallback in `RunAgent`
3. **#3** — Check `Available()` in `TaskRouter.Route`
4. **#4** — Add retry logic to `processWorkerResults`

### Priority 2 — Correctness (fix soon)

1. **#5** — Prune `assignments` when workers finish
2. **#6** — Fix `path` handling in `detectCycleDFS`
3. **#7** — Replace `time.After` with `time.NewTimer` + defer
4. **#8** — Select on both contexts in worker goroutine
5. **#9** — Cap blackboard history

### Priority 3 — Maintainability (next sprint)

1. **#10** — Decompose Queen into smaller files
2. **#11** — Extract shared constraint injection
3. **#12** — Extract shared task persistence
4. **#13** — Reuse safety guard
5. **#14** — Fix `KillAll` logging
6. **#15** — Fix `Clear()` to reset history

### Priority 4 — Polish (backlog)

1. **#16-#23** — Type safety, perf micro-opts, test coverage, UTF-8
