# Safety and Blocking Implementation Plan

## Execution Status

- Phase 1: Completed
- Phase 2: Completed
- Phase 3: Completed
- Phase 4: Completed

## Context and Decisions

- macOS is first-class for local development and CI.
- Safety should default to strict behavior.
- A permissive mode should be available as an explicit option.
- Blocking should be reliable and action-oriented (based on executable intent), not text-snippet matching on prompts.

## Objectives

1. Fix path validation correctness and security issues.
2. Implement strict-by-default safety mode with a permissive fallback mode.
3. Replace fragile command substring blocking with structured command blocking.
4. Ensure behavior is testable and stable on macOS and Linux CI.
5. Preserve usability with clear diagnostics and controllable policy.

## Non-Goals

- No broad redesign of orchestration architecture.
- No changes to unrelated adapters or task semantics beyond safety enforcement.
- No behavior changes outside safety enforcement without explicit need.

## Current Risks to Address First

1. `internal/safety/safety.go`: path checks can fail valid macOS temp paths and may allow prefix-based bypasses.
2. `internal/adapter/generic.go`: command blocking currently inspects task description text, which causes false positives and is bypassable.
3. Mixed expectations between strict safety and current permissive runtime behavior.

## Target Behavior

### Path Safety

- Normalize and resolve both allowed paths and requested paths consistently.
- Use boundary-safe containment checks (`filepath.Rel`) instead of string prefix matching.
- Handle macOS `/var` <-> `/private/var` canonicalization reliably.

### Blocking Scope

- Enforce blocking on executable command intent:
  - `exec` adapter script body (`PromptAsScript`)
  - explicit `task.Context["command"]` when present
- Do not block based on natural-language task descriptions for non-script adapters.

### Safety Modes

- `strict` (default):
  - deny on parse failures for script/command inspection
  - deny blocked executable/rule matches
  - deny dangerous indirection unless allowlisted
- `permissive`:
  - allow on parse failures but emit warnings
  - block only high-confidence destructive matches
  - still enforce path boundary checks

## Config Changes

Add to `SafetyConfig` in `internal/config/config.go`:

- `Mode string` with values: `strict`, `permissive` (default: `strict`)
- `EnforceOnAdapters []string` (default: `["exec"]`)
- `BlockedExecutables []string`
- `BlockedPatterns []string` (argument/pattern rules)
- `AllowExecutables []string` (optional override)

Backward compatibility:

- If new fields are unset, preserve safe defaults and existing behavior where possible.

## Blocking Engine Design

### Recommended Approach

- Introduce a safety inspection component in `internal/safety`:
  - Parse shell command/script into command invocations.
  - Evaluate executable + argument-aware rules.
  - Return typed verdicts (`allow`, `deny`, `warn`) with reason codes.

### Parser Strategy

- Prefer a shell parser for correctness (third-party justified here).
- If parser fails:
  - strict mode: deny
  - permissive mode: warn and continue

### Rule Evaluation

- Hard-block executable list: `sudo`, `mkfs`, etc.
- Argument-aware blocks:
  - `rm -rf /`
  - destructive `dd` targets
- Detect risky indirection (`eval`, `bash -c`, command substitution) per mode policy.

## Phased Implementation

### Phase 1: Critical Path Safety Fixes (PR 1)

Files:

- `internal/safety/safety.go`
- `internal/safety/safety_test.go`
- `internal/adapter/safety_integration_test.go`
- `internal/queen/tools_test.go`
- `internal/queen/tools_extra_test.go`

Tasks:

1. Replace prefix checks with canonical boundary checks.
2. Canonicalize allowed paths and requested paths safely.
3. Add explicit tests for macOS canonical path behavior and sibling-prefix bypass.
4. Ensure existing failing tests pass.

Acceptance:

- `go test ./internal/safety ./internal/queen ./internal/adapter` passes.
- Path traversal and sibling-prefix bypass tests fail correctly.
- macOS temp-dir path tests pass consistently.

### Phase 2: Strict/Permissive Mode Framework (PR 2)

Files:

- `internal/config/config.go`
- `internal/safety/safety.go`
- `internal/safety/safety_test.go`
- `README.md` (config docs)

Tasks:

1. Add mode and policy fields to config.
2. Implement strict/permissive behavior for safety checks.
3. Document defaults and migration behavior.

Acceptance:

- Defaults to strict mode.
- Explicit permissive mode changes only intended enforcement branches.

### Phase 3: Structured Command Blocking (PR 3)

Files:

- `internal/safety/*` (new blocking engine files)
- `internal/adapter/generic.go`
- `internal/adapter/*_test.go`

Tasks:

1. Implement command/script inspection engine.
2. Move blocking decisions off task description text for non-script adapters.
3. Enforce on `exec` scripts and explicit command context.
4. Add detailed deny/warn reason messaging.

Acceptance:

- False positives from plain-text descriptions are eliminated.
- Blocked command tests pass for script/command execution paths.
- Strict/permissive parse-failure behavior is covered by tests.

### Phase 4: Tooling and Hardening (PR 4)

Files:

- `.github/workflows/ci.yml`
- add `.golangci.yml`
- targeted lint/test files

Tasks:

1. Add `staticcheck` and `govulncheck` to CI.
2. Resolve current `golangci-lint` findings for errcheck/gosimple.
3. Add race-focused test runs for safety/adapter/queen paths.

Acceptance:

- CI enforces fmt, vet, race tests, static analysis, and vuln scan.
- No ignored critical errors in production code paths.

## Test Plan

### Unit Tests

- Path containment:
  - inside allowed dir
  - sibling prefix (`/repo` vs `/repo2`)
  - traversal attempts
  - symlink scenarios
  - macOS canonical path equivalents
- Blocking:
  - blocked executable match
  - blocked arg pattern match
  - parse failure behavior by mode
  - indirection behavior by mode

### Integration Tests

- Adapter safety integration for `exec` and non-script adapters.
- Queen `read_file` and `list_files` tool calls under safety constraints.

### CI Matrix

- Ensure tests run on:
  - macOS runner
  - Linux runner

## Operational Rollout

1. Merge Phase 1 immediately (critical correctness/security).
2. Merge Phase 2 with strict default + documented permissive override.
3. Merge Phase 3 with structured blocking and migration notes.
4. Enable stricter CI gates in Phase 4 after codebase is clean.

## Success Criteria

- No known path-boundary bypasses.
- No macOS-specific false rejections in path checks.
- Blocking decisions are deterministic, explainable, and mode-aware.
- Strict mode is safe by default; permissive mode is explicit and observable.
- Safety-related tests are stable and green in macOS and Linux CI.
