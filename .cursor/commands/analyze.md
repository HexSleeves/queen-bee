# Analyze for Optimizations

## Overview

Analyze the provided files or selection to identify optimization opportunities in Go, following idiomatic patterns and best practices for senior-level engineering.

## Focus Areas

### Go Idioms & Type Safety

- [ ] Verify adherence to Effective Go and Go Code Review Comments guidelines
- [ ] Identify unnecessary use of `interface{}` / `any` where concrete types or generics suffice
- [ ] Review generic usage (Go 1.18+) for proper type constraints and inference
- [ ] Check for proper use of type assertions and type switches with exhaustive handling
- [ ] Assess custom type definitions for domain clarity (e.g., `type UserID int64`)
- [ ] Look for opportunities to use sentinel types or enums via `iota`
- [ ] Review struct embedding for proper composition over inheritance
- [ ] Check for proper use of pointer vs value receivers with consistency
- [ ] Identify redundant type conversions or unnecessary allocations
- [ ] Evaluate whether exported types and functions truly need to be exported

### Performance Optimizations

- [ ] Identify unnecessary heap allocations (escape analysis awareness)
- [ ] Check for proper use of `sync.Pool` for frequently allocated objects
- [ ] Review goroutine lifecycle management and leaks
- [ ] Assess caching strategies and I/O patterns (buffered readers/writers)
- [ ] Look for opportunities to preallocate slices and maps with `make(T, len, cap)`
- [ ] Evaluate use of `sync.Map` vs mutex-guarded maps based on access patterns
- [ ] Check for proper use of `strings.Builder` over repeated string concatenation
- [ ] Review database query patterns (connection pooling, prepared statements, `rows.Close()`)
- [ ] Identify hot paths that would benefit from profiling (`pprof`)
- [ ] Check for unnecessary use of reflection where generics or code generation would suffice

### Concurrency Patterns

- [ ] Verify proper use of channels vs mutexes for the given use case
- [ ] Check for goroutine leaks (missing context cancellation, unbounded spawning)
- [ ] Review `context.Context` propagation through the call chain
- [ ] Assess `select` statements for proper timeout and cancellation handling
- [ ] Look for race conditions (candidates for `-race` flag verification)
- [ ] Evaluate `errgroup` usage for structured concurrent work
- [ ] Check for proper channel direction annotations (`chan<-`, `<-chan`)

### Error Handling

- [ ] Check for unchecked errors (missing `if err != nil` or ignored returns)
- [ ] Review error wrapping with `fmt.Errorf("...: %w", err)` for context
- [ ] Assess use of custom error types and `errors.Is` / `errors.As`
- [ ] Look for sentinel errors where appropriate (`var ErrNotFound = errors.New(...)`)
- [ ] Evaluate panic usage — should be limited to truly unrecoverable situations
- [ ] Check that deferred cleanup functions (e.g., `resp.Body.Close()`) handle errors

### Code Quality & Maintainability

- [ ] Check for SOLID principles adherence and clean package boundaries
- [ ] Review for DRY violations and abstraction opportunities via interfaces
- [ ] Assess interface design — prefer small interfaces accepted, concrete types returned
- [ ] Look for proper separation of concerns across packages
- [ ] Evaluate test coverage, table-driven test patterns, and testability via dependency injection
- [ ] Review `go vet`, `staticcheck`, and `golangci-lint` compliance
- [ ] Check for proper struct field ordering to minimize padding
- [ ] Assess documentation — exported symbols should have godoc comments
- [ ] Review dependency management (`go.mod` tidiness, minimal dependencies)

## Output Format

For each identified optimization:

1. **Location**: File and line number
2. **Current Code**: Brief snippet of the issue
3. **Issue**: Description of the problem
4. **Recommendation**: Specific fix with code example
5. **Impact**: Performance, correctness, maintainability, or readability improvement
6. **Priority**: High / Medium / Low

## Guidelines

- Prioritize changes that have measurable impact (benchmark or profile-backed when possible)
- Suggest incremental improvements that don't require massive refactors
- Focus on patterns that improve the entire codebase, not just one-off fixes
- Consider the existing codebase patterns, module structure, and conventions
- Balance optimization with readability — "clear is better than clever"
- Only suggest generics (Go 1.18+) or newer features (e.g., `log/slog` in 1.21, range-over-func in 1.23) if the project's `go.mod` targets a compatible version
- Prefer stdlib solutions over third-party libraries unless the dependency is well-justified
