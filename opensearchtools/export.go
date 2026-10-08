package opensearchtools

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	stdos "os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/disaster37/opensearch/v4"
	"github.com/disaster37/opensearch/v4/api"
	"github.com/disaster37/opensearch/v4/querydsl"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/timberio/go-datemath"
	"github.com/urfave/cli/v2"
)

// exportBatchSize is the number of documents fetched per search batch.
const exportBatchSize = 5000

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
	progress          *exportProgress  // shared cross-goroutine counters; nil only in direct unit-test calls
	os                opensearch.Client
}

// exportStats accumulates export counters across batches and (for the
// --open-index path) across indexes.
type exportStats struct {
	found    int64 // total documents found on OpenSearch (TotalHits)
	exported int64 // documents actually written after filtering
}

// exportProgress holds counters written by the export goroutine and read by the
// signal-handler goroutine at interrupt time. Fields are atomic so the two
// goroutines never race and the export hot path stays lock-free. Use only
// through a pointer; do not copy the struct after first use (atomic values must
// not be copied).
type exportProgress struct {
	found    atomic.Int64 // total documents found (TotalHits), accumulated across indexes on the open-index path
	exported atomic.Int64 // documents written after filtering; updated once per batch
}

// formatExportSummary formats the canonical filtered-export summary phrase.
func formatExportSummary(exported, found int64) string {
	return fmt.Sprintf("Exported %d documents after filtering (from %d found)", exported, found)
}

// formatPerIndexSummary formats the per-index filtered-export summary phrase.
func formatPerIndexSummary(exported, found int64, indexName string) string {
	return fmt.Sprintf("%s for index %s", formatExportSummary(exported, found), indexName)
}

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

// ExportDataToFiles permit to extract some datas to files
// It return error if something wrong
func ExportDataToFiles(c *cli.Context) error {
	os, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	from := c.String("from")
	to := c.String("to")
	dateField := c.String("date-field")
	index := c.String("index")
	query := c.String("query")
	fields := c.StringSlice("fields")
	separator := c.String("separator")
	splitFileField := c.String("split-file-field")
	path := c.String("path")
	isOpenClosedIndex := c.Bool("open-index")
	pitDuraction := c.String("pit-duration")
	compress := c.Bool("compress")

	if path == "" {
		return errors.New("You must set --path")
	}
	if splitFileField == "" {
		return errors.New("You must set --split-file-field")
	}
	if query == "" {
		return errors.New("You must set --query")
	}

	filters, err := compileFilters(c.StringSlice("filters"))
	if err != nil {
		return err
	}

	opts := exportOptions{
		fromDate:          from,
		toDate:            to,
		dateField:         dateField,
		index:             index,
		query:             query,
		isOpenClosedIndex: isOpenClosedIndex,
		fields:            fields,
		separator:         separator,
		splitFileColumn:   splitFileField,
		path:              path,
		pitDuration:       pitDuraction,
		compress:          compress,
		filters:           filters,
		os:                os,
	}

	if err = exportDataToFiles(c.Context, opts); err != nil {
		return err
	}

	log.Infof("Extract successfully")

	return nil
}

func exportDataToFiles(ctx context.Context, opts exportOptions) error {
	if opts.path == "" {
		return errors.New("You must provide path")
	}
	if opts.index == "" {
		return errors.New("You must provide index")
	}

	if opts.dateField == "" {
		return errors.New("You must provide date-field")
	}

	if opts.os == nil {
		return errors.New("You must provide es client")
	}

	if _, err := datemath.Parse(opts.fromDate); err != nil {
		return errors.Wrapf(err, "error to parse date %s", opts.fromDate)
	}

	if _, err := datemath.Parse(opts.toDate); err != nil {
		return errors.Wrapf(err, "error to parse date %s", opts.toDate)
	}

	log.Debugf("fromDate: %s", opts.fromDate)
	log.Debugf("toDate: %s", opts.toDate)
	log.Debugf("dateField: %s", opts.dateField)
	log.Debugf("index: %s", opts.index)
	log.Debugf("query: %s", opts.query)
	log.Debugf("fields: %s", opts.fields)
	log.Debugf("separator: %s", opts.separator)
	log.Debugf("splitFileColumn: %s", opts.splitFileColumn)
	log.Debugf("path: %s", opts.path)
	log.Debugf("isOpenClosedIndex: %t", opts.isOpenClosedIndex)
	log.Debugf("pitDuration: %s", opts.pitDuration)
	log.Debugf("compress: %t", opts.compress)

	opts.progress = &exportProgress{} // set before any goroutine is spawned

	// Search on all index, without needed to work index by index
	if !opts.isOpenClosedIndex {
		// Register signal handler to flush buffered writers on Ctrl+C / kill.
		// This branch has no metadata cleanup; flushing is the only shutdown work.
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

	// Work index by index
	return exportDataToFilesWithClosedIndex(ctx, opts)
}

func exportDataToFilesWithoutClosedIndex(ctx context.Context, opts exportOptions) (stats exportStats, err error) {
	// Normalize query
	opts.query = querydsl.NormalizeLuceneQuery(opts.query)

	// Build query
	rangeDateQuery := querydsl.NewRangeQuery(opts.dateField).
		Gte(opts.fromDate).
		Lte(opts.toDate)
	stringQuery := querydsl.NewQueryStringQuery(opts.query).WithAnalyzeWildcard(true)
	boolQuery := querydsl.NewBoolQuery().Must(rangeDateQuery, stringQuery)

	// Forge payload
	computedFields := append(opts.fields, opts.splitFileColumn)
	pitResponse, err := opts.os.Search().CreatePIT(
		ctx,
		&api.CreatePITRequest{
			Indices:   []string{opts.index},
			KeepAlive: opts.pitDuration,
		},
	)
	if err != nil {
		return stats, errors.Wrap(err, "error to create PIT")
	}
	defer func() {
		_, err := opts.os.Search().DeletePIT(ctx, &api.DeletePITRequest{
			PitIds: []string{pitResponse.PitId},
		})
		if err != nil {
			log.Errorf("error to delete PIT: %v", err)
		}
	}()

	reqDsl := querydsl.NewSearchRequest().
		Query(boolQuery).
		Size(exportBatchSize).
		Sort(opts.dateField, true).
		Sort("_shard_doc", true).
		FetchSourceContext(querydsl.NewFetchSourceContext(true).Include(computedFields...)).
		TrackTotalHits(true).
		PointInTime(querydsl.NewPointInTime(pitResponse.PitId))

	// Get records over scroll
	firstLoop := true

	// fileWriterCache keeps buffered writers open across all batches.
	// On replicated/network storage (e.g. Longhorn), unbuffered per-line
	// writes and reopening files on every batch are extremely costly.
	// Buffering collapses thousands of write() syscalls per batch into a few,
	// and keeping file handles open avoids repeated open/stat/close.
	cache := newFileWriterCache(opts.compress)
	defer func() {
		if cerr := cache.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	log.Infof("Start search on Opensearch with index %s and search '%s", opts.index, opts.query)
	for {
		req, err := api.NewSearchRequest(reqDsl)
		if err != nil {
			return stats, errors.Wrap(err, "error to create search request")
		}

		searchResult, err := opts.os.Search().Search(ctx, req)
		if err != nil {
			return stats, err
		}

		if len(searchResult.Hits.Hits) == 0 {
			break
		}

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

		reqDsl.SearchAfter(searchResult.Hits.Hits[len(searchResult.Hits.Hits)-1].Sort...)
	}

	return stats, nil
}

func exportDataToFilesWithClosedIndex(ctx context.Context, opts exportOptions) error {
	fromDateExpr, err := datemath.Parse(opts.fromDate)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", opts.fromDate)
	}
	fromDateTime := fromDateExpr.Time()

	toDateExpr, err := datemath.Parse(opts.toDate)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", opts.toDate)
	}
	toDateTime := toDateExpr.Time()

	// Create metadata index
	if err = createMetadataindexIfNotExist(ctx, opts.os); err != nil {
		return err
	}

	// Get current user
	authResponse, err := opts.os.Security().AuthInfo(ctx)
	if err != nil {
		return errors.Wrap(err, "error to get current user")
	}

	// Create metadata document
	metadata := &Metadata{
		Type:      MetadataTypeExportAutoOpenIndex,
		User:      authResponse.UserName,
		SessionId: uuid.New().String(),
		Indexes:   []string{},
	}
	if err = createMetdata(ctx, metadata, opts.os); err != nil {
		return errors.Wrap(err, "error to create metadata document")
	}

	defer func() {
		// Delete metadata document
		if err := cleanMetadataExportWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, opts.os); err != nil {
			log.Errorf("error to delete metadata document %s: %v", metadata.Id, err)
		}
	}()

	// Register signal handler for graceful cleanup on Ctrl+C / kill
	sigCh := make(chan stdos.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
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
	defer signal.Stop(sigCh)

	log.Infof("Loop over index in datastream %s to found the starting index", opts.index)

	var total exportStats
	fn := func(ctx context.Context, indexName string) error {
		s, err := handleClosedIndex(ctx, metadata, indexName, opts)
		if err != nil {
			return err
		}
		total.found += s.found
		total.exported += s.exported
		log.Infof("%s", formatPerIndexSummary(s.exported, s.found, indexName))
		return nil
	}

	if err := forEachIndexInDateRange(ctx, opts.os, opts.index, fromDateTime, toDateTime, fn); err != nil {
		return err
	}

	log.Infof("%s", formatExportSummary(total.exported, total.found))

	return nil
}

// handleClosedIndex permit to open index if it's closed, process it and close it if it's not used by another session
func handleClosedIndex(ctx context.Context, metadata *Metadata, index string, opts exportOptions) (stats exportStats, err error) {
	if _, err = lockIndex(ctx, index, metadata, opts.os); err != nil {
		return stats, errors.Wrapf(err, "error to lock index %s", index)
	}
	defer func() {
		// unlock index; never mask the export error with the unlock result
		if uerr := unlockIndex(ctx, index, metadata, opts.os); uerr != nil {
			log.Errorf("Error when unlock index %s: %s", index, uerr.Error())
			if err == nil {
				err = uerr
			}
		}
	}()

	opts.index = index
	return exportDataToFilesWithoutClosedIndex(ctx, opts)
}

// fileWriterCache holds buffered writers keyed by file path, kept open for the
// whole export so that file handles are not reopened on every batch and writes
// are buffered instead of issuing one write() syscall per document line.
type fileWriterCache struct {
	compress bool
	files    map[string]*stdos.File
	gzips    map[string]*gzip.Writer
	writers  map[string]*bufio.Writer
}

const fileWriterBufferSize = 256 * 1024

// writerCacheRegistry tracks all live caches so that signal handlers can flush
// their buffers before the process exits. Because exits go through os.Exit,
// deferred flushes are bypassed; flushing the registry preserves buffered data.
var (
	writerCacheRegistryMu sync.Mutex
	writerCacheRegistry   = make(map[*fileWriterCache]struct{})
)

func registerWriterCache(c *fileWriterCache) {
	writerCacheRegistryMu.Lock()
	writerCacheRegistry[c] = struct{}{}
	writerCacheRegistryMu.Unlock()
}

func unregisterWriterCache(c *fileWriterCache) {
	writerCacheRegistryMu.Lock()
	delete(writerCacheRegistry, c)
	writerCacheRegistryMu.Unlock()
}

// flushAllWriterCaches flushes buffered data of every live cache. It is meant to
// be called from signal handlers right before os.Exit so that no buffered lines
// are lost on interruption.
func flushAllWriterCaches() {
	writerCacheRegistryMu.Lock()
	defer writerCacheRegistryMu.Unlock()
	for c := range writerCacheRegistry {
		c.flush()
	}
}

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

func newFileWriterCache(compress bool) *fileWriterCache {
	c := &fileWriterCache{
		compress: compress,
		files:    make(map[string]*stdos.File),
		gzips:    make(map[string]*gzip.Writer),
		writers:  make(map[string]*bufio.Writer),
	}
	registerWriterCache(c)
	return c
}

// get returns a buffered writer for fileName, opening the underlying file on
// first use and reusing the same handle for subsequent batches.
func (c *fileWriterCache) get(fileName string) (*bufio.Writer, error) {
	if w, ok := c.writers[fileName]; ok {
		return w, nil
	}

	if _, err := stdos.Stat(fileName); stdos.IsNotExist(err) {
		log.Infof("Create file: %s", fileName)
	}
	log.Debugf("Open file %s", fileName)

	file, err := stdos.OpenFile(fileName, stdos.O_APPEND|stdos.O_CREATE|stdos.O_WRONLY, 0o644)
	if err != nil {
		log.Errorf("Error when open file: %s", err.Error())
		return nil, err
	}

	var w *bufio.Writer
	if c.compress {
		gz := gzip.NewWriter(file)
		w = bufio.NewWriterSize(gz, fileWriterBufferSize)
		c.gzips[fileName] = gz
	} else {
		w = bufio.NewWriterSize(file, fileWriterBufferSize)
	}
	c.files[fileName] = file
	c.writers[fileName] = w

	return w, nil
}

// flush flushes every buffered writer without closing the files. It is safe to
// call from a signal handler to persist buffered data before exiting.
func (c *fileWriterCache) flush() {
	for name, w := range c.writers {
		if err := w.Flush(); err != nil {
			log.Errorf("Error when flush file %s: %s", name, err.Error())
		}
	}
	for name, gz := range c.gzips {
		if err := gz.Flush(); err != nil {
			log.Errorf("Error when flush gzip writer %s: %s", name, err.Error())
		}
	}
}

// Close flushes every buffered writer and closes all open files. It returns the
// first error encountered while still attempting to close the remaining files.
func (c *fileWriterCache) Close() error {
	defer unregisterWriterCache(c)

	var firstErr error
	for name, w := range c.writers {
		if err := w.Flush(); err != nil {
			log.Errorf("Error when flush file %s: %s", name, err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	for name, gz := range c.gzips {
		if err := gz.Close(); err != nil {
			log.Errorf("Error when close gzip writer %s: %s", name, err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	for name, f := range c.files {
		if err := f.Close(); err != nil {
			log.Errorf("Error when close file %s: %s", name, err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func processExport(searchResult *querydsl.SearchResult, opts exportOptions, cache *fileWriterCache) (written int, err error) {
	log.Debugf("Process %d documents", len(searchResult.Hits.Hits))

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
			return written, err
		}
		if _, err = fmt.Fprintf(writer, "%s\n", line); err != nil {
			log.Errorf("Error when write file: %s", err.Error())
			return written, err
		}
		written++
	}
	if filtered > 0 {
		log.Debugf("Filtered out %d of %d documents in this batch", filtered, len(searchResult.Hits.Hits))
	}

	return written, nil
}
