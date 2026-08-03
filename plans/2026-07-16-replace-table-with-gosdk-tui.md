# Replace Local Table With gosdk/tui Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with review checkpoints.

**Goal:** Remove the duplicated `utils.Table` implementation and make `svc/stat` use `github.com/bizshuk/gosdk/tui` directly without changing report output.

**Architecture:** `svc/stat/format.go` will construct `tui.Table` and `tui.Cell` values directly. The `utils` package will no longer expose table types, and the SDK remains the single table-rendering implementation.

**Tech Stack:** Go 1.26.3, `github.com/bizshuk/gosdk/tui`, Go test, Go build.

## Global Constraints

- Preserve existing table output and `FormatReport` behavior.
- Do not add a compatibility alias or another local table implementation.
- Preserve unrelated existing modifications in `go.mod` and `go.sum`.
- Validate with `go test ./...` and `go build -o bin/skills .`.

---

### Task 1: Establish baseline and deletion check

**Files:**
- Read: `utils/table.go`
- Read: `utils/table_test.go`
- Read: `svc/stat/format.go`

- [x] **Step 1: Run the existing table and stat tests**

Run:

```bash
go test ./utils ./svc/stat
```

Expected: exit code 0 before the refactor.

- [x] **Step 2: Write the structural red check**

Run:

```bash
test ! -e utils/table.go
```

Expected: fail because the local implementation currently exists. This is the red check for the requested removal.

### Task 2: Migrate the consumer to gosdk/tui

**Files:**
- Modify: `svc/stat/format.go`
- Delete: `utils/table.go`
- Delete: `utils/table_test.go`

**Interfaces:**
- Consumes: `tui.Table`, `tui.Cell`, and `(*tui.Table).Draw(io.Writer, bool, bool)` from `github.com/bizshuk/gosdk/tui`.
- Produces: unchanged `FormatReport(io.Writer, *StatsResult)` output behavior.

- [x] **Step 1: Change the import and table type references**

In `svc/stat/format.go`, replace:

```go
"github.com/bizshuk/skills/utils"
```

with:

```go
"github.com/bizshuk/gosdk/tui"
```

Then replace every `utils.Table` with `tui.Table` and every `utils.Cell` with `tui.Cell`.

- [x] **Step 2: Remove the duplicated local implementation and its implementation test**

Delete `utils/table.go` and `utils/table_test.go`. Do not change the table data construction or rendering arguments in `svc/stat/format.go`.

- [x] **Step 3: Run the structural red check again as the green check**

Run:

```bash
test ! -e utils/table.go && ! -e utils/table_test.go && ! rg -q 'utils\.(Table|Cell)' svc/stat/format.go
```

Expected: exit code 0.

- [x] **Step 4: Run focused tests**

Run:

```bash
go test ./utils ./svc/stat
```

Expected: exit code 0; `utils` has no table test remaining and `svc/stat` compiles against `gosdk/tui`.

### Task 3: Verify the complete migration

**Files:**
- Verify: `go.mod`
- Verify: `go.sum`
- Verify: all Go packages

- [x] **Step 1: Confirm the SDK package resolves**

Run:

```bash
go list github.com/bizshuk/gosdk/tui
```

Expected: prints `github.com/bizshuk/gosdk/tui`.

- [x] **Step 2: Run all tests**

Run:

```bash
go test ./...
```

Expected: exit code 0 with no failed packages.

- [x] **Step 3: Build the CLI**

Run:

```bash
go build -o bin/skills .
```

Expected: exit code 0 and a compiled `bin/skills` binary.

- [x] **Step 4: Review the diff**

Run:

```bash
git diff --stat && git diff -- svc/stat/format.go utils/table.go utils/table_test.go
```

Expected: only the intended import/type migration and local table file deletions are present; pre-existing `go.mod` and `go.sum` changes remain untouched.
