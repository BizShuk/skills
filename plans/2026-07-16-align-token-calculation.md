# Align Token Calculation Implementation Plan

> **For agentic workers:** Execute this plan task-by-task with review checkpoints.

**Goal:** Align Claude token accounting with `ccstatusline`, separate cache tokens, and invalidate incompatible historical caches.

**Architecture:** Add explicit cache fields to `UsageStats`; parse Claude usage entries through a request-level selector before aggregating by hour. Keep report aggregation and rendering in `svc/stat`, with cache version validation at the persistence boundary.

**Tech Stack:** Go 1.26.3, JSONL, Go testing, `gosdk/tui`.

## Global Constraints

- Preserve Codex and Antigravity parsing behavior except for explicit cache classification.
- Do not convert old mixed-input cache values; invalidate them by schema version.
- Total must equal Input + Cached + Output.
- Verify with focused tests, `go test ./...`, and `go build -o bin/skills .`.

---

### Task 1: Add failing tests for token semantics and streaming selection

**Files:**
- Create: `svc/stat/token_test.go`

- [x] **Step 1: Add tests for separated cache fields**

Test `UsageStats.AddTokenUsage` with input `100`, cache read `20`, cache creation `10`, output `5`, and assert all fields plus total equal `135`.

- [x] **Step 2: Add tests for final-entry selection**

Create two entries with the same request ID: one unfinished and one final. Assert only the final entry remains. Create an unfinished-only request with multiple entries and assert only the latest remains. Assert entries without request IDs remain unchanged.

- [x] **Step 3: Run focused tests and verify RED**

Run `go test ./svc/stat -run 'Test(UsageStats|SelectClaude)' -count=1`.

Expected: compilation failure because the new method, fields, entry type, and selector do not exist yet.

### Task 2: Implement explicit token accounting and Claude de-duplication

**Files:**
- Modify: `svc/stat/model.go`
- Modify: `svc/stat/parse.go`
- Modify: `svc/stat/stat_test.go`
- Create: `svc/stat/token_test.go`

- [x] **Step 1: Add cache fields and `AddTokenUsage`**

Keep `AddTokens(input, output)` for non-cache callers, and add `AddTokenUsage(input, cacheRead, cacheCreation, output)` that updates four separate counters. Update `Merge` and `TotalTokens` to include all four counters.

- [x] **Step 2: Add request-level Claude entry selection**

Parse Claude token records into a small internal entry type containing request ID, sequence, timestamp, model, stop-reason state, and four token values. Group records with a non-empty request ID; select the last final record per group, or the latest record if no final record exists. Leave records without request IDs unchanged.

- [x] **Step 3: Apply selected Claude records after file scanning**

Collect usage entries while scanning the file, select them after the scan, then filter by target date and call `AddTokenUsage`. Keep tool/skill extraction restricted to the target date.

- [x] **Step 4: Classify Codex cache tokens**

Call `AddTokenUsage(input, cached_input_tokens, 0, output)` for Codex token events. Keep Antigravity on `AddTokens`.

- [x] **Step 5: Run focused tests and verify GREEN**

Run `go test ./svc/stat -run 'Test(UsageStats|SelectClaude)' -count=1`.

Expected: PASS.

### Task 3: Invalidate old cache schema and update reports

**Files:**
- Modify: `svc/stat/model.go`
- Modify: `svc/stat/cache.go`
- Modify: `svc/stat/service.go`
- Modify: `svc/stat/format.go`
- Modify: `svc/stat/stat_test.go`

- [x] **Step 1: Version `DayStats` cache data**

Set the current version in `NewDayStats`; have `LoadCache` reject any other version so `Run` reparses and rewrites old cache files.

- [x] **Step 2: Aggregate cache totals**

Add total cache-read and cache-creation fields to `StatsResult`, and aggregate them alongside input/output.

- [x] **Step 3: Update report columns**

Add `Cached` to the overall summary and daily table; compute Total as input + cache read + cache creation + output.

- [x] **Step 4: Update formatting tests**

Assert `formatToken(454)` remains the existing project behavior unless explicitly changed; test report data uses separate cached values and includes them in Total.

- [x] **Step 5: Run all verification**

Run:

```bash
go test ./...
go build -o bin/skills .
git diff --check
```

Expected: all tests pass, build exits 0, and diff check emits no errors.
