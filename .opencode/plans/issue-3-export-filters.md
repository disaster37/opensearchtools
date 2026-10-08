# Implementation Plan — Issue #3: `--filters` (soft grep) for `export-data`

## 1. Context & goal

`opensearchtools export-data` rebuilds log files from documents returned by an OpenSearch
query. A common need is to only extract a field that is **not indexed** on OpenSearch, i.e.
filter the extracted lines locally with a regex before writing them to disk.

This plan adds a repeatable `--filters` flag to `export-data` that accepts Go (RE2) regular
expressions. A document's extracted output line (the `--fields` values joined by
`--separator`) is written to its file **only if** it matches at least one filter. When no
filters are provided, behavior is byte-for-byte identical to today.

Key invariant that drives the implementation order: **filtering must happen before any file
is created/opened**, so non-matching documents never produce empty files (see §6.3).

## 2. Design decisions & assumptions

1. **What a filter matches against — the extracted output line.**
   Each compiled regex is matched against `strings.Join(extractedFields, separator)` — exactly
   the text that would be written to the file. Rationale: this mirrors `grep` (which matches
   the line), it is the simplest mental model, and it lets a single pattern match across
   multiple extracted fields (including the separator). The alternative — per-field matching —
   was rejected because it would require associating each regex with a specific field and adds
   config surface that the issue does not ask for.

2. **Multiple filters: OR semantics (grep `-e` style).**
   A line is kept if it matches **any** compiled filter. Rationale: `grep -e a -e b` keeps
   lines matching `a` OR `b`, and "soft grep equivalent" is the stated intent. AND semantics
   would be surprising for a filter list and is not directly expressible in Go's RE2 engine
   (no lookahead), so AND is explicitly out of scope and documented.

3. **Flag name/type: `--filters`, `cli.StringSliceFlag` (repeatable).**
   No default value. This matches the existing `--fields` and `--labels` StringSlice flags for
   consistency. **No env/config (altsrc) support** — the other `export-data` flags are plain
   `cli.*Flag` (altsrc is only used for global flags), and the issue does not ask for it.
   - **Known urfave/cli caveat (must be documented in README):** `StringSliceFlag` splits each
     flag value on commas and trims surrounding whitespace. A single `--filters` value that
     contains a comma (e.g. a `{m,n}` quantifier like `error{2,3}`) will be silently split into
     two patterns. Workaround: avoid commas (prefer `{m}` or alternation) and repeat
     `--filters` for multiple patterns. Leading/trailing literal spaces in a pattern are also
     trimmed (use `\s` or `[ ]` if a literal space at an edge is required).

4. **Invalid regex handling: fail fast, compile once.**
   All patterns are validated and compiled **once**, up front in `ExportDataToFiles`, before any
   OpenSearch call (PIT/search) or file write. Any invalid pattern returns a clear error
   immediately. Compiled `*regexp.Regexp` values are threaded through the export as
   `[]*regexp.Regexp` so no recompilation happens per batch/document.

5. **Empty / whitespace-only filter values: reject.**
   After `strings.TrimSpace`, an empty value is an error (fail fast). Rationale: `regexp.Compile("")`
   succeeds and matches everything, which would silently disable filtering and is almost
   certainly a user mistake. Rejecting is clearer than silently ignoring.

6. **Filtering happens BEFORE file creation/opening.** See §6.3. This is the one behavioral
   reordering in `processExport` (extract line → filter → determine filename → open writer).

7. **Backward compatibility.** `len(filters) == 0` ⇒ `matchAnyFilter` returns `true` for every
   line with no measurable overhead (a single `len` check). Existing exports produce identical
   output.

8. **Interaction with other options.**
   - `--compress`: filtering is independent of compression; the filter never sees the `.gz`
     suffix (suffix is added only after a line passes the filter).
   - `--split-file-field` / `unknown_host` fallback: the split-field value is only consulted
     **after** a line passes the filter, so an empty split-field value still maps to
     `unknown_host` but no file is created if the line is filtered out.
   - Multi-field extraction: the filter sees the whole joined line including separators; a
     pattern may legitimately include the separator (e.g. `ERROR\|500`).

9. **Refactor the long parameter list into an options struct.** YES. The 13-positional-parameter
   signature is unwieldy and growing. Introduce `exportOptions` (see §3) and thread it by value.
   `ExportDataToFiles` (public CLI entry) keeps its signature.

10. **Logging (found + filtered counts).** Keep the existing Info log
    `Found %d document to export` (`TotalHits.Value`) emitted in
    `exportDataToFilesWithoutClosedIndex` on the first batch. Add a new Info-level
    "exported after filtering" summary:
    `Exported %d documents after filtering (from %d found)`.
    `processExport` keeps its per-batch **Debug** log
    `Filtered out N of M documents in this batch`.

11. **Aggregation: single final Info log (not per-index).** For the `--open-index` path, the
    exported/found counts from each index are aggregated and logged **once** at the end of
    `exportDataToFilesWithClosedIndex`. Rationale: one summary line is the least noisy, and the
    per-index `Found N document to export` logs already give the per-index breakdown. For the
    non-open-index path the same single summary is emitted in `exportDataToFiles` after the
    search completes.

## 3. Data structures (exact)

Add to `opensearchtools/export.go` (package `opensearchtools`):

```go
// exportOptions carries all configuration needed to run an export. It replaces
// the previous 13-positional-parameter signatures and is passed by value down
// the export call chain.
type exportOptions struct {
	fromDate          string
	toDate            string
	dateField         string
	index             string
	query             string
	isOpenClosedIndex bool
	fields            []string
	separator         string
	splitFileColumn   string
	path              string
	pitDuration       string
	compress          bool
	filters           []*regexp.Regexp // compiled filter regexes; nil/empty = no filtering
	os                opensearch.Client
}

// exportStats accumulates export counters across batches and (for the
// --open-index path) across indexes.
type exportStats struct {
	found    int64 // total documents found on OpenSearch (TotalHits)
	exported int64 // documents actually written after filtering
}
```

Add a package-level constant (replaces the local `size := 5000` in `exportDataToFiles`):

```go
const exportBatchSize = 5000
```

Add two pure helper functions (unit-testable without OpenSearch):

```go
// compileFilters validates and compiles raw filter patterns. It returns
// (nil, nil) for an empty pattern list, a clear error for an empty/whitespace
// pattern, and a wrapped error for an invalid regex.
func compileFilters(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for i, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, errors.Errorf("filter %d is empty", i+1)
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid filter regex %q (filter %d)", p, i+1)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// matchAnyFilter reports whether line matches at least one compiled filter.
// An empty filter slice means "keep every line" (no filtering).
func matchAnyFilter(filters []*regexp.Regexp, line string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, re := range filters {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
```

Notes:
- `regexp.Compile` / `re.MatchString` give RE2 "substring match" semantics, matching `grep`.
- `errors` in `export.go` is `github.com/pkg/errors` (already imported); use `errors.Errorf` /
  `errors.Wrapf`, not `fmt.Errorf`.
- Add `"regexp"` to the `export.go` import block.

## 4. Function signatures (exact, after refactor)

In `opensearchtools/export.go`:

```go
// Public CLI entry — signature unchanged.
func ExportDataToFiles(c *cli.Context) error

func exportDataToFiles(ctx context.Context, opts exportOptions) error

func exportDataToFilesWithoutClosedIndex(ctx context.Context, opts exportOptions) (stats exportStats, err error)

func exportDataToFilesWithClosedIndex(ctx context.Context, opts exportOptions) error

func handleClosedIndex(ctx context.Context, metadata *Metadata, index string, opts exportOptions) (stats exportStats, err error)

func processExport(searchResult *querydsl.SearchResult, opts exportOptions, cache *fileWriterCache) (written int, err error)
```

Behavioral notes for each:

- `ExportDataToFiles`: read all flags including `filters := c.StringSlice("filters")`; call
  `compileFilters(filters)`; keep the existing `path`/`split-file-field`/`query` non-empty
  checks; build `exportOptions`; call `exportDataToFiles(c.Context, opts)`. In the
  non-open-index branch, capture the returned `stats` and emit the final Info summary
  `Exported %d documents after filtering (from %d found)`.

- `exportDataToFiles`: keep existing validations (path, index, date-field, os non-nil, datemath
  parse of from/to) but read from `opts`. Branch on `opts.isOpenClosedIndex` as today.

- `exportDataToFilesWithoutClosedIndex`: use `exportBatchSize` for the search size; set
  `stats.found = searchResult.Hits.TotalHits.Value` once on the first batch; accumulate
  `stats.exported += int64(written)` from each `processExport` call; return `stats`.

- `exportDataToFilesWithClosedIndex`: keep a local `var total exportStats`; the per-index
  callback becomes:

  ```go
  fn := func(ctx context.Context, indexName string) error {
      s, err := handleClosedIndex(ctx, metadata, indexName, opts)
      if err != nil {
          return err
      }
      total.found += s.found
      total.exported += s.exported
      return nil
  }
  ```

  After `forEachIndexInDateRange` returns, emit the single aggregate Info summary
  `Exported %d documents after filtering (from %d found)`.

- `handleClosedIndex`: set `opts.index = index` on its local (by-value) copy, then
  `return exportDataToFilesWithoutClosedIndex(ctx, opts)`. It returns the per-index `stats`
  and keeps the existing deferred-unlock error semantics unchanged.

- `processExport`: reordered loop body (see §6.3); returns `written` = number of lines actually
  written (equals `len(hits) - filtered`).

## 5. File-by-file change list

### 5.1 `opensearchtools/export.go`

1. Add `"regexp"` to the import block (keep `errors` = `github.com/pkg/errors`).
2. Add `const exportBatchSize = 5000`, the `exportOptions` struct, and the `exportStats` struct (§3).
3. Add `compileFilters` and `matchAnyFilter` helpers (§3).
4. Rewrite `ExportDataToFiles` to read `filters`, compile them, build `exportOptions`, and call
   the new `exportDataToFiles(ctx, opts)`; in the non-open-index branch log the final
   `Exported ... after filtering` summary from the returned stats.
5. Rewrite `exportDataToFiles`, `exportDataToFilesWithoutClosedIndex`,
   `exportDataToFilesWithClosedIndex`, `handleClosedIndex` to the `exportOptions`/`exportStats`
   signatures in §4. `querySize`/`size := 5000` is replaced by `exportBatchSize`.
   `exportDataToFilesWithoutClosedIndex` sets `stats.found` once and accumulates
   `stats.exported`; `exportDataToFilesWithClosedIndex` aggregates per-index stats and logs one
   final summary.
6. Rewrite `processExport` with the filtering step (see §6.3), the Debug filtered-count log, and
   return the `written` count.
7. Leave `fileWriterCache`, `flushAllWriterCaches`, signal handling, and the writer
   registry **unchanged**.

### 5.2 `main.go`

In the `export-data` command `Flags` slice (currently lines 121–182), add after the `fields`
flag (lines 152–156):

```go
&cli.StringSliceFlag{
    Name:  "filters",
    Usage: "Go regex to keep only matching output lines (repeatable, OR semantics). Matches the extracted fields joined by --separator",
},
```

No change to the `Action` (still `localopensearch.ExportDataToFiles`).

### 5.3 `opensearchtools/export_test.go`

1. Update the three existing `exportDataToFiles(...)` call sites (lines 24, 47, 70) to build
   `exportOptions{...}` (see §7.2 for the exact pattern) and pass `exportDataToFiles(ctx, opts)`.
2. Add one new integration test `TestExportDataToFilesWithFilters` (§7.2).

### 5.4 New test files `opensearchtools/filters_test.go` and `opensearchtools/export_unit_test.go`

Pure unit tests for `compileFilters`/`matchAnyFilter` and `processExport` (no OpenSearch).
See §7.1.

### 5.5 `README.md`

Update the "Export data" section (§8).

## 6. Edge cases, error handling & validation rules

### 6.1 Validation (all in `ExportDataToFiles`, fail fast, before any search/file write)
- `--path`, `--split-file-field`, `--query` non-empty (existing behavior preserved).
- `compileFilters(c.StringSlice("filters"))`:
  - empty slice → `(nil, nil)` → no filtering;
  - whitespace-only value → error `filter N is empty`;
  - invalid regex → error `invalid filter regex "..." (filter N)` wrapping the `regexp` error.
- `exportDataToFiles` still validates path/index/date-field/os non-nil and parses from/to with
  `datemath.Parse` (existing behavior preserved).

### 6.2 Filtering semantics
- OR across filters; `re.MatchString` (substring) semantics.
- No filters ⇒ keep every line (no behavior change).

### 6.3 Filtering before file creation (critical ordering)
`processExport` reordered loop body:

```go
filtered := 0
for _, item := range searchResult.Hits.Hits {
    jsonResult := gjson.ParseBytes(item.Source)

    // 1. Build the output line first.
    td := make([]string, 0, len(opts.fields))
    for _, field := range opts.fields {
        td = append(td, jsonResult.Get(field).Str)
    }
    line := strings.Join(td, opts.separator)

    // 2. Filter BEFORE determining filename / opening any file.
    if !matchAnyFilter(opts.filters, line) {
        filtered++
        continue
    }

    // 3. Determine target file (unchanged logic).
    var fileName string
    if jsonResult.Get(opts.splitFileColumn).Str == "" {
        log.Debugf("No value for field %s on document id %s", opts.splitFileColumn, item.Id)
        fileName = fmt.Sprintf("%s/unknown_host", opts.path)
    } else {
        safeValue := filepath.Base(jsonResult.Get(opts.splitFileColumn).Str)
        fileName = fmt.Sprintf("%s/%s", opts.path, safeValue)
    }
    if opts.compress {
        fileName += ".gz"
    }

    // 4. Open writer (creates file only here) and write.
    writer, err := cache.get(fileName)
    if err != nil {
        return err
    }
    _, err = fmt.Fprintf(writer, "%s\n", line)
    if err != nil {
        log.Errorf("Error when write file: %s", err.Error())
        return err
    }
}
if filtered > 0 {
    log.Debugf("Filtered out %d of %d documents in this batch", filtered, len(searchResult.Hits.Hits))
}
```

Note: `gjson.ParseBytes` is called once per hit; `item.Source` parsing behavior is unchanged.
On success `processExport` returns `written = len(searchResult.Hits.Hits) - filtered`; any
open/write error returns a partial `written` plus the error (callers discard counts on error).

### 6.4 Count correctness (found vs. exported)
- `found` (`stats.found`, int64) comes from `searchResult.Hits.TotalHits.Value` and is captured
  **once per index/search** on the first batch. It is the query total, not a per-batch count, so
  it must NOT be summed per batch.
- `exported` (`stats.exported`, int64) is the sum of `processExport`'s `written` values across
  batches (`written == len(hits) - filtered` per batch).
- With `--open-index`, `exportDataToFilesWithClosedIndex` sums each index's stats into a single
  `exportStats` and logs the aggregate once. For the single-index path the same summary is
  logged in `exportDataToFiles`.
- The final summary is emitted even when nothing is found (reports `0`), so the "after filter"
  number is always visible. The per-index `Found N document to export` log is unchanged.

## 7. Testing strategy

### 7.1 Unit tests (run WITHOUT OpenSearch)

New file `opensearchtools/filters_test.go`, `package opensearchtools`, plain `Test*` functions
(not on `ESTestSuite`), using `github.com/stretchr/testify/assert`.

- `TestCompileFilters`
  - Empty/nil input: `filters, err := compileFilters(nil)` → `assert.NoError`, `assert.Nil(filters)`.
  - Valid patterns (`[]string{"error", "^warning$", "foo.*bar"}`): no error, length 3, and each
    compiled regex non-nil.
  - Whitespace-only (`[]string{"   "}`): `assert.Error`, error contains `"empty"`.
  - Empty string (`[]string{""}`): `assert.Error`, error contains `"empty"`.
  - Invalid regex (`[]string{"[unclosed"}`): `assert.Error`, error contains `"invalid filter regex"`.
  - Leading/trailing whitespace trimmed (`[]string{"  error  "}` compiles and matches `"error"`).

- `TestMatchAnyFilter`
  - No filters (`nil` and `[]*regexp.Regexp{}`): returns `true` for any line.
  - Single filter match / no match (`regexp.MustCompile("error")` vs `"an error occurred"` → true;
    vs `"all good"` → false).
  - OR semantics: `[]*regexp.Regexp{regexp.MustCompile("error"), regexp.MustCompile("warn")}`
    matches `"warn message"` even though `"error"` does not.
  - Case sensitivity (default): `"Error"` vs pattern `"error"` → false (documents RE2 default).
  - Separator-aware: pattern `"a\\|b"` matches the joined line `"a|b"`.

New file `opensearchtools/export_unit_test.go`, `package opensearchtools`, plain `Test*` function
(no OpenSearch), using `github.com/stretchr/testify/assert`. It builds an in-memory
`*querydsl.SearchResult` (no client) and a temp-dir-backed `*fileWriterCache`:

- `TestProcessExport`
  - Build a `querydsl.SearchResult` with 3 hits whose `Source` is raw JSON containing
    `message` + `node_name` fields, e.g.
    `{"message":"error A","node_name":"es-0"}`, `{"message":"ok B","node_name":"es-1"}`,
    `{"message":"error C","node_name":"es-0"}`.
  - `opts := exportOptions{fields: []string{"message"}, separator: "|", splitFileColumn: "node_name",
    path: <tempDir>, filters: []*regexp.Regexp{regexp.MustCompile("error")}}`.
  - `cache := newFileWriterCache(false)`; `defer cache.Close()`.
  - `written, err := processExport(sr, opts, cache)`.
  - Assert: `assert.NoError`, `assert.Equal(2, written)` (only the two "error" docs),
    `<tempDir>/es-0` exists and contains `"error A\nerror C\n"`, and
    `<tempDir>/es-1` does **not** exist (`os.IsNotExist`) — proving no empty file for the
    filtered-out doc.
  - Note: `log` output is **not** asserted.

### 7.2 Integration tests (require OpenSearch — added to `export_test.go`)

Update the three existing tests to the struct form. Example (TestExportDataToFiles):

```go
opts := exportOptions{
    fromDate:          "now-1000y",
    toDate:            "now",
    dateField:         "@timestamp",
    index:             "logs",
    query:             "*",
    isOpenClosedIndex: false,
    fields:            []string{"message"},
    separator:         "|",
    splitFileColumn:   "node_name",
    path:              dir,
    pitDuration:       "24h",
    compress:          false,
    os:                s.client,
}
err = exportDataToFiles(context.Background(), opts)
```

(TestExportDataToFilesWithOpenIndex uses `index: "test"`, `isOpenClosedIndex: true`; the
compressed test uses `compress: true` — mirroring current values.)

New `TestExportDataToFilesWithFilters`:

- Build `opts` as above with `index: "logs"`, `fields: []string{"message"}`,
  `splitFileColumn: "node_name"`, and `filters: []*regexp.Regexp{regexp.MustCompile("334ms")}`.
- Call `stats, err := exportDataToFilesWithoutClosedIndex(context.Background(), opts)`
  (this returns the counters; `exportDataToFiles` only returns an error).
- Assert:
  - `assert.NoError(s.T(), err)`.
  - `assert.Equal(s.T(), int64(2), stats.found)` — both fixture docs found.
  - `assert.Equal(s.T(), int64(1), stats.exported)` — only the `334ms` doc survives the filter.
  - `dir/es-0` exists and equals `"[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n"`.
  - `dir/es-1` does **not** exist (`_, err := os.ReadFile(dir+"/es-1")`; `assert.True(os.IsNotExist(err))`)
    — this validates the "no empty file" invariant.
- Note: log output (the Info "Found"/"Exported" lines) is **not** asserted.

(The fixture `fixtures/logs/bulk.ndjson` contains exactly these two docs with `node_name`
`es-0` / `es-1` and messages `334ms` / `279ms`.)

## 8. Documentation updates (`README.md`)

In the "Export data" parameters list (after `fields`), add:

```markdown
  - **filters**: Go regular expression(s) to keep only matching output lines before writing.
    Repeatable (`--filters "a" --filters "b"`); a line is kept if it matches ANY filter (OR
    semantics, like `grep -e`). The pattern is matched against the extracted fields joined by
    `--separator`. Note: because the CLI splits each `--filters` value on commas, avoid comma
    characters in a single pattern (e.g. `{2,3}` quantifiers); repeat the flag instead.
```

Add a second sample command demonstrating filters:

```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate export-data --from now-12h --to now --index "logs-*" --query "*" --fields log.original --filters "ERROR" --filters "5[0-9][0-9]" --path /tmp
```

At Info level, the export logs the number of documents found on OpenSearch
(`Found N document to export`) and, after applying `--filters`, the number actually written
(`Exported N documents after filtering (from M found)`). With `--open-index`, the
`Exported ...` summary is a single aggregate across all processed indexes.

## 9. Git workflow

From the main checkout (`/projects/opensearchtools`, currently on `3.x`):

```bash
# 1. Create a worktree at a sibling path based on origin/3.x (fallback local 3.x)
git fetch origin
git worktree add ../opensearchtools-issue-3 origin/3.x   # if origin/3.x missing: `git worktree add ../opensearchtools-issue-3 3.x`
cd ../opensearchtools-issue-3

# 2. Create the feature branch
git switch -c feat/issue-3-export-filters

# 3. Make the changes, then commit
git add -A
git commit -m "feat(export): add --filters to filter exported lines before write (#3)"

# 4. Push and open PR to base 3.x referencing issue #3
git push -u origin feat/issue-3-export-filters
# Open PR: base = 3.x, head = feat/issue-3-export-filters; body references "Fixes #3".
```

## 10. Exact verification commands

```bash
# In the worktree (cd ../opensearchtools-issue-3):
go build ./...
go vet ./...
gofmt -l .          # must print nothing

# Unit tests ONLY (no OpenSearch needed):
go test ./opensearchtools -run 'TestCompileFilters|TestMatchAnyFilter|TestProcessExport' -v

# Full test suite (requires OpenSearch at opensearch.svc:9200; use Dagger which spins it up):
dagger call --src . test
# or, with a manually-available cluster (override host/user/pass via env):
OPENSEARCH_HOST=... OPENSEARCH_USERNAME=... OPENSEARCH_PASSWORD=... go test ./...
```

## 11. Step-by-step implementation checklist

1. `export.go`: add `"regexp"` import.
2. `export.go`: add `const exportBatchSize = 5000`, the `exportOptions` struct, and the
   `exportStats` struct.
3. `export.go`: add `compileFilters` and `matchAnyFilter`.
4. `export.go`: rewrite `ExportDataToFiles` (read `--filters`, compile, build opts; log the
   final `Exported ... after filtering` summary in the non-open-index branch).
5. `export.go`: rewrite `exportDataToFiles` + the three nested funcs to the
   `exportOptions`/`exportStats` signatures, accumulating/aggregating counts (§6.4).
6. `export.go`: rewrite `processExport` with extract→filter→filename→write ordering, the Debug
   log, and return the `written` count.
7. `main.go`: add the `--filters` `cli.StringSliceFlag`.
8. `export_test.go`: convert 3 existing call sites to `exportOptions`; add
   `TestExportDataToFilesWithFilters` (asserts `stats.found`/`stats.exported` + file contents).
9. Add `filters_test.go` (`TestCompileFilters`, `TestMatchAnyFilter`) and
   `export_unit_test.go` (`TestProcessExport`).
10. `README.md`: add `filters` parameter doc + example command + a note on the found/exported
    logs.
11. Run §10 verification commands (gofmt/vet/build/unit tests; full suite via Dagger).
12. Follow §9 git workflow to commit, push, and open the PR to `3.x`.

## 12. Assumptions & open questions

- **Assumption:** "grep equivalent" means substring-match against the joined output line (not a
  full-line match). If full-line matching (`^...$`) is desired, users can anchor their regex.
- **Assumption:** OR semantics for multiple filters (already recommended above).
- **Assumption:** the "after filtering" count is the number of output lines actually written; the
  "found" count is `TotalHits.Value` (query total). A single aggregate Info summary is emitted
  per export (see §2.10–§2.11).
- **Documented limitation:** urfave/cli `StringSliceFlag` comma-splitting / whitespace-trimming
  (see §2.3) is not worked around in code; it is documented in README.
