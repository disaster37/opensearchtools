# Plan — Per-index export statistics & interrupt summary

- **Branch:** `feat/export-per-index-stats` (from `3.x`)
- **Component:** `opensearchtools export-data`
- **Scope:** per-index Info summary in `--open-index` path; interrupt-time aggregate stats in both paths; README; tests.
- **Out of scope:** `--filters` semantics, per-batch Debug log, per-index summary in non-open path, progress bars/ETA, file naming/compression/PIT/metadata behavior, logging stats on index error.

---

## 1. Goal & behavior contract

- After each backing index finishes (open-index path), log:
  `Exported N documents after filtering (from M found) for index <index-name>`
- On SIGINT/SIGTERM (both paths), after flushing: log
  `Exported N documents after filtering (from M found) before interruption`
  then metadata cleanup (open-index path), then `os.Exit(1)`.
- Success output/exit codes unchanged (final aggregate, `Found N document to export`, `Extract successfully`).
- Per-batch `Filtered out N of M documents in this batch` remains Debug; no new flags.

---

## 2. Design decisions

### 2.1 Race-free counters: `exportProgress` (`sync/atomic`)

New struct with **two independent `atomic.Int64` fields** (NOT a mutex). Justification:

- The export goroutine is the sole writer; the signal-handler goroutine is an occasional, terminal reader. `atomic.AddInt64`/`atomic.Int64.Load` is lock-free — so no blocking, no lock contention on the 5000-doc batch hot path, and no risk of holding a lock across an I/O boundary.
- A mutex would need a per-batch `Lock/Unlock` and careful discipline in a path that ends in `os.Exit(1)`; atomic needs none.
- The pair `(found, exported)` is read with two separate `Load()`s, so the snapshot may tear by one increment between them. This is acceptable: both counters are monotonic, so the reported pair is a valid (found, exported) pair at some instant near the interrupt. Best-effort, per spec edge case "interrupt mid-batch".

```go
// exportProgress holds counters written by the export goroutine and read by the
// signal-handler goroutine at interrupt time. Fields are atomic so the two
// goroutines never race and the export hot path stays lock-free. Use only
// through a pointer; do not copy the struct after first use (atomic values must
// not be copied).
type exportProgress struct {
    found    atomic.Int64 // total documents found (TotalHits), accumulated across indexes on the open-index path
    exported atomic.Int64 // documents written after filtering; updated once per batch
}
```

Counter update policy (satisfies "including completed batches of the index in progress"):

- `found`: `Add(TotalHits.Value)` on the first batch of each index (where `stats.found` is set today). On the open-index path this accumulates across indexes; on the non-open path it is set once.
- `exported`: `Add(written)` after every batch.

### 2.2 Threading the counter to the handlers

Add one pointer field to `exportOptions` (created in `exportDataToFiles` **before** any goroutine starts). `exportOptions` is passed by value everywhere, so the pointer propagates to both branches, to `fn`/`handleClosedIndex`/`exportDataToFilesWithoutClosedIndex` (open path), and into the handler closures. This is the smallest possible churn vs. adding a new parameter to two more signatures.

```go
type exportOptions struct {
    // ...existing fields...
    filters  []*regexp.Regexp
    progress *exportProgress // shared cross-goroutine counters; nil when not tracked
    os       opensearch.Client
}
```

`exportDataToFilesWithoutClosedIndex` is also called directly from a test (`TestExportDataToFilesWithFilters`), so every access must be nil-guarded.

### 2.3 Per-index summary emission point (US1)

In the `fn` callback of `exportDataToFilesWithClosedIndex`, **after** `handleClosedIndex` returns successfully (which is after `Close index %s` from the deferred `unlockIndex`, and before `forEachIndexInDateRange` logs the next `Process index %s`). Index name = the `indexName` callback parameter, which is the **backing index** name (e.g. `.ds-test-000001`), consistent with the existing `Process index %s` and `Found N document to export` per-index logs. Not emitted on error (export aborts as today).

### 2.4 Interrupt summary + ordering (US2)

Extract the ordered shutdown sequence so it can be unit-tested:

```go
// runInterruptShutdown performs the ordered interrupt sequence: flush buffered
// writers, log accumulated stats, then run cleanup (metadata cleanup on the
// open-index path). The caller must call os.Exit(1) afterwards. Split from the
// signal listener so its side effects and ordering are testable.
func runInterruptShutdown(progress *exportProgress, cleanup func()) {
    flushAllWriterCaches()
    logInterruptSummary(progress)
    if cleanup != nil {
        cleanup()
    }
}

// logInterruptSummary logs the accumulated counters at interrupt time.
func logInterruptSummary(progress *exportProgress) {
    if progress == nil {
        log.Infof("%s before interruption", formatExportSummary(0, 0))
        return
    }
    log.Infof("%s before interruption", formatExportSummary(progress.exported.Load(), progress.found.Load()))
}
```

For the non-open path, `cleanup` is `nil` (no metadata session exists). For the open-index path, `cleanup` closes over `authResponse`, `metadata`, and `opts.os`, calling the existing `cleanMetadataExportWithAutoOpenIndex`.

### 2.5 DRY helper

```go
// formatExportSummary formats the canonical filtered-export summary phrase.
func formatExportSummary(exported, found int64) string {
    return fmt.Sprintf("Exported %d documents after filtering (from %d found)", exported, found)
}
```

Used in all four log sites (final aggregate ×2, per-index ×1, interrupt ×1). Output is byte-identical to today's strings (no behavior change), and gives a single point to unit-test the exact wording.

### 2.6 Error handling

No new error paths. All new logging uses `logrus` as elsewhere. No `pkg/errors` additions. Counters are best-effort and cannot fail.

---

## 3. File-by-file changes (with sketches)

### 3.1 `opensearchtools/export.go`

**Imports:** add `"sync/atomic"` (keep existing `"sync"`).

**(a) Add `exportProgress` + `formatExportSummary` + `logInterruptSummary` + `runInterruptShutdown`** (see §2.1/§2.4/§2.5). Place `exportProgress` and `formatExportSummary` near `exportStats` (~line 56); place the two interrupt helpers near `flushAllWriterCaches` (~line 441).

**(b) `exportOptions`:** add `progress *exportProgress` field.

**(c) `exportDataToFiles` — non-open branch:**

```go
func exportDataToFiles(ctx context.Context, opts exportOptions) error {
    // ...existing validation & debug logs unchanged...

    opts.progress = &exportProgress{} // set before any goroutine is spawned

    if !opts.isOpenClosedIndex {
        sigCh := make(chan stdos.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        defer signal.Stop(sigCh)
        go func() {
            <-sigCh
            log.Warn("Received termination signal, flushing buffered data...")
            runInterruptShutdown(opts.progress, nil)
            stdos.Exit(1)
        }()

        stats, err := exportDataToFilesWithoutClosedIndex(ctx, opts)
        if err != nil {
            return err
        }
        log.Infof("%s", formatExportSummary(stats.exported, stats.found))
        return nil
    }

    return exportDataToFilesWithClosedIndex(ctx, opts)
}
```

**(d) `exportDataToFilesWithoutClosedIndex` — counter updates:**

```go
if firstLoop {
    firstLoop = false
    stats.found = searchResult.Hits.TotalHits.Value
    if opts.progress != nil {
        opts.progress.found.Add(searchResult.Hits.TotalHits.Value)
    }
    log.Infof("Found %d document to export", searchResult.Hits.TotalHits.Value)
}

written, err := processExport(searchResult, opts, cache)
if err != nil {
    return stats, err
}
stats.exported += int64(written)
if opts.progress != nil {
    opts.progress.exported.Add(int64(written))
}
```

**(e) `exportDataToFilesWithClosedIndex` — handler + per-index summary:**

Replace the signal-handler goroutine body:

```go
go func() {
    <-sigCh
    log.Warn("Received termination signal, cleaning up...")
    runInterruptShutdown(opts.progress, func() {
        if cleanErr := cleanMetadataExportWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, opts.os); cleanErr != nil {
            log.Errorf("Error during cleanup: %v", cleanErr)
        }
    })
    stdos.Exit(1)
}()
```

Replace `fn` body:

```go
fn := func(ctx context.Context, indexName string) error {
    s, err := handleClosedIndex(ctx, metadata, indexName, opts)
    if err != nil {
        return err
    }
    total.found += s.found
    total.exported += s.exported
    log.Infof("%s for index %s", formatExportSummary(s.exported, s.found), indexName)
    return nil
}
```

Replace final aggregate:

```go
log.Infof("%s", formatExportSummary(total.exported, total.found))
```

### 3.2 `opensearchtools/export_unit_test.go`

Add pure unit tests (no OpenSearch). New helpers are in the same package.

- `TestFormatExportSummary`: assert exact strings for `(0,0)`, `(2,3)`.
- `TestExportProgress`: start at 0/0; `Add` found/exported; `Load` returns values; concurrent `Add`+`Load` across goroutines (run under `-race`).
- `TestLogInterruptSummary`: redirect `logrus` output to a `bytes.Buffer` (restore in defer); assert nil => `... (from 0 found) before interruption`, non-nil => correct counts.
- `TestRunInterruptShutdownOrdering`: capture logrus output; inject a `cleanup` that appends a sentinel line to the same buffer (e.g. `log.Infof("cleanup done")`); assert the summary line precedes the cleanup line and `cleanup` ran. Optionally create a `fileWriterCache`, write an unflushed line, and assert the file contains it after `runInterruptShutdown` (verifies flush-before-cleanup). Use `defer cache.Close()`.

### 3.3 `README.md`

Extend the paragraph at lines 129–132 (don't touch the stray curl lines after it):

```markdown
At Info level, the export logs the number of documents found on OpenSearch
(`Found N document to export`) and, after applying `--filters`, the number actually written
(`Exported N documents after filtering (from M found)`). With `--open-index`, the
`Exported ...` summary is a single aggregate across all processed indexes, and each index
also logs its own `Exported N documents after filtering (from M found) for index <name>` as
it completes. If the export is interrupted (Ctrl+C/SIGTERM), the accumulated counts are
logged as `Exported N documents after filtering (from M found) before interruption` and the
tool exits with code 1.
```

---

## 4. Test plan

| Layer | What | How |
|---|---|---|
| Unit (pure) | summary formatting, progress add/load + race, interrupt summary, shutdown ordering | `export_unit_test.go`; run with `-race` |
| Integration (ESTestSuite) | **No changes required.** Existing tests assert file contents + returned `exportStats`; they now also exercise the new counter updates + per-index log without asserting log text. | Run via dagger |
| Signal handling | In-process SIGINT is impractical (`os.Exit(1)` kills the test binary) — covered instead by `runInterruptShutdown`/`logInterruptSummary` unit tests | N/A |

Optional (non-blocking) integration enhancement: in `TestExportDataToFilesWithOpenIndex`, wrap a logrus hook and assert at least one `for index ` line and one final aggregate line. Not required for merge.

---

## 5. Risks & edge-case handling

- **`atomic.Int64` must not be copied** — `exportProgress` is always used via pointer; only the pointer field inside `exportOptions` is copied. Documented in struct comment.
- **Snapshot tear between `found`/`exported` `Load()`s** — benign; monotonic counters guarantee a valid pair at interrupt time.
- **Interrupt before first batch** — `found=0, exported=0` (first batch never sets `found`).
- **0-hit index** — per-index summary `0/0`; no `Found` line (unchanged).
- **All filtered out** — `found=M, exported=0`.
- **Index errors** — `fn` returns before logging the per-index summary; export aborts (unchanged).
- **`flAllWriterCaches` bypass of defers under `os.Exit`** — unchanged; handler still explicitly flushes + cleans.
- **Multiple signals** — buffered chan (size 1); first `os.Exit(1)` terminates; subsequent behavior unchanged.
- **Nil `progress` in direct `exportDataToFilesWithoutClosedIndex` calls** — all accesses nil-guarded.
- **Perf** — one `atomic.AddInt64` per batch (not per doc); negligible.

---

## 6. Verification commands (from `CONTRIBUTE.md` + repo)

Local (no OpenSearch needed):

```bash
go build ./...
go vet ./...
gofmt -l .                       # expect no output
go test ./opensearchtools/ -run 'TestProcessExport|TestCompileFilters|TestMatchAnyFilter|TestFormatExportSummary|TestExportProgress|TestLogInterruptSummary|TestRunInterruptShutdown' -race -count=1
```

Full CI (build + lint + format + OpenSearch integration tests) via dagger:

```bash
dagger call --src . lint
dagger call --src . format export --path .
dagger call --src . test
dagger call --src . ci export --path .   # full pipeline
```

Note: `go test ./...` cannot run locally without an OpenSearch at `opensearch.svc` — the `ESTestSuite.SetupSuite` blocks waiting for it. Always use the targeted `-run` filter or dagger for the full suite.

---

## 7. Ordered task list (for the Coder Agent)

1. Add `"sync/atomic"` import to `export.go`.
2. Add `exportProgress` struct + `formatExportSummary` helper.
3. Add `progress *exportProgress` field to `exportOptions`.
4. Add `logInterruptSummary` + `runInterruptShutdown` helpers.
5. In `exportDataToFiles`: create `opts.progress`; swap non-open handler body to `runInterruptShutdown(opts.progress, nil)`; use `formatExportSummary` for final aggregate.
6. In `exportDataToFilesWithoutClosedIndex`: nil-guarded `found.Add` + `exported.Add`.
7. In `exportDataToFilesWithClosedIndex`: swap handler to `runInterruptShutdown(opts.progress, cleanupFn)` with metadata cleanup closure; add per-index log in `fn`; use `formatExportSummary` for final aggregate.
8. Add unit tests to `export_unit_test.go`.
9. Update `README.md` paragraph.
10. Run verification commands (§6).

---

## 8. Open questions

None blocking. (Optional non-blocking: whether to add the logrus-hook assertion to the open-index integration test — recommended to skip for now.)