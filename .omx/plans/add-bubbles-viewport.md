# Add Charmbracelet Bubbles `viewport` to the TUI (Queen + Worker)

Date: 2026-02-17  
Owner: executor  
Estimated complexity: MEDIUM

## Context

Waggle already uses Bubble Tea + Lip Gloss, but Queen/Worker panels implement scrolling manually (`queenScroll` / `workerScroll`) and slice raw lines before wrapping. This creates mismatch between scroll math and what’s actually visible when long lines wrap.

Goal: adopt `github.com/charmbracelet/bubbles/viewport` for **both** the Queen panel and the Worker output panel, **default-on**, keeping the current overall layout and keybindings with only minor scroll-semantic drift allowed.

## Work objectives

1. Add `github.com/charmbracelet/bubbles/viewport` and integrate it into the TUI.
2. Replace custom scroll offsets with viewport-driven scrolling for Queen + Worker panels.
3. Preserve current layout, borders, and navigation keys (`tab`, `shift+tab`, `left/right`, `d`, `0`, `q`, `ctrl+c`).
4. Keep current “auto-follow newest output” behavior (Queen always snaps to bottom on new output; Worker snaps to bottom when viewing that worker).

## Guardrails

### Must have
- Default-on (no new CLI flag required).
- Visual-line-correct scrolling (scrolling is based on wrapped output, not raw lines).
- Resizing the terminal reflows content without panics or negative sizes.

### Must NOT have
- No broad UI redesign (Task panel, DAG view, input prompt remain as-is).
- No change to output modes (`--plain` / TUI routing) beyond what’s needed for viewport integration.

## Task flow (3–6 steps)

### 1) Add dependency + introduce viewport state

Update:
- `go.mod` / `go.sum`: add `github.com/charmbracelet/bubbles` (for `viewport`).
- `/Users/jacob.lecoq.ext/Projects/CLI-Tools/queen-bee/internal/tui/model.go`: add viewport model fields (Queen + Worker) and any minimal helpers needed.

Acceptance criteria:
- `go test ./...` passes.
- `go build ./cmd/waggle/` succeeds.

### 2) Queen panel: render + scroll via viewport

Update:
- `/Users/jacob.lecoq.ext/Projects/CLI-Tools/queen-bee/internal/tui/view.go`:
  - Replace manual slicing/padding in `renderQueenPanel` with a `viewport.Model` that renders the wrapped content.
  - Keep existing title line content; place viewport content below it.
  - Reuse existing `wrapText` to pre-wrap lines to viewport width (ensure width accounts for border/padding).

Behavior requirements:
- `up/k` and `down/j` scroll the Queen panel by visual lines.
- On new Queen output (thinking/tool/result/log/done messages), the Queen viewport snaps to bottom (current behavior).

Acceptance criteria:
- With long wrapped lines, scrolling moves smoothly row-by-row (no “jumping” due to raw-line slicing).
- The existing Queen title + border styling remains intact.

### 3) Worker panel: render + scroll via viewport

Update:
- `/Users/jacob.lecoq.ext/Projects/CLI-Tools/queen-bee/internal/tui/view.go`:
  - Replace manual slicing/padding in `renderWorkerOutputPanel` with a `viewport.Model`.
  - When switching workers (via `tab`/`shift+tab` or `left/right`), keep current behavior: reset scroll to bottom for the newly selected worker.

Behavior requirements:
- `up/k` and `down/j` scroll the Worker panel by visual lines when in worker view.
- On new Worker output for the currently viewed worker, snap to bottom (current behavior).

Acceptance criteria:
- Worker output scroll is visual-line-correct on wrapped lines.
- Switching workers still shows “latest output” by default.

### 4) Key handling + resize handling

Update:
- `/Users/jacob.lecoq.ext/Projects/CLI-Tools/queen-bee/internal/tui/model.go`:
  - Route `up/k` and `down/j` into the active viewport (Queen vs Worker) instead of mutating `queenScroll` / `workerScroll`.
  - On `tea.WindowSizeMsg`, update viewport width/height and re-wrap content deterministically.
  - Keep input-mode key handling unchanged.

Acceptance criteria:
- Existing navigation keys continue to work as before.
- No panics on very small terminals; viewport sizes are clamped to safe minimums.

### 5) Validation + regression checks

Add or adjust tests (only if there’s a stable seam):
- Prefer testing any new helper(s) that build wrapped viewport content and implement “snap to bottom” behavior.

Run:
- `gofmt -w .`
- `go vet ./...`
- `go test ./...`
- `go build ./cmd/waggle/`

Manual checks:
- Start waggle in TUI mode, generate enough output to scroll in Queen and Worker panels, verify `up/k` / `down/j` behavior and that new output snaps to bottom.
- Resize the terminal narrower/wider while scrolled and verify no rendering corruption/panics.

## Success criteria

- Queen + Worker panels use `bubbles/viewport` for scrolling, default-on.
- Scrolling is correct on wrapped visual lines.
- Keys and layout remain consistent with the current UX.
- `go vet ./...`, `go test ./...`, and `go build ./cmd/waggle/` all succeed.

## Notes / follow-ups (explicitly out of scope unless discovered)

- If worker output includes ANSI escape codes, `wrapText` may mis-measure widths. If this shows up in practice, plan a follow-up to add ANSI-aware wrapping/stripping.

