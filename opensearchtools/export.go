package opensearchtools

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	stdos "os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
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

	err = exportDataToFiles(c.Context, from, to, dateField, index, query, isOpenClosedIndex, fields, separator, splitFileField, path, pitDuraction, compress, os)
	if err != nil {
		return err
	}

	log.Infof("Extract successfully")

	return nil
}

func exportDataToFiles(ctx context.Context, fromDate string, toDate string, dateField string, index string, query string, isOpenClosedIndex bool, fields []string, separator string, splitFileColumn string, path string, pitDuration string, compress bool, os opensearch.Client) error {
	if path == "" {
		return errors.New("You must provide path")
	}
	if index == "" {
		return errors.New("You must provide index")
	}

	if dateField == "" {
		return errors.New("You must provide date-field")
	}

	if os == nil {
		return errors.New("You must provide es client")
	}

	if _, err := datemath.Parse(fromDate); err != nil {
		return errors.Wrapf(err, "error to parse date %s", fromDate)
	}

	if _, err := datemath.Parse(toDate); err != nil {
		return errors.Wrapf(err, "error to parse date %s", toDate)
	}

	size := 5000

	log.Debugf("fromDate: %s", fromDate)
	log.Debugf("toDate: %s", toDate)
	log.Debugf("dateField: %s", dateField)
	log.Debugf("index: %s", index)
	log.Debugf("query: %s", query)
	log.Debugf("fields: %s", fields)
	log.Debugf("separator: %s", separator)
	log.Debugf("splitFileColumn: %s", splitFileColumn)
	log.Debugf("path: %s", path)
	log.Debugf("isOpenClosedIndex: %t", isOpenClosedIndex)
	log.Debugf("pitDuration: %s", pitDuration)
	log.Debugf("compress: %t", compress)

	// Search on all index, without needed to work index by index
	if !isOpenClosedIndex {
		// Register signal handler to flush buffered writers on Ctrl+C / kill.
		// This branch has no metadata cleanup; flushing is the only shutdown work.
		sigCh := make(chan stdos.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		go func() {
			<-sigCh
			log.Warn("Received termination signal, flushing buffered data...")
			flushAllWriterCaches()
			stdos.Exit(1)
		}()

		return exportDataToFilesWithoutClosedIndex(ctx, size, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, pitDuration, compress, os)
	} else {
		// Work index by index
		return exportDataToFilesWithClosedIndex(ctx, size, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, pitDuration, compress, os)
	}
}

func exportDataToFilesWithoutClosedIndex(ctx context.Context, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, pitDuration string, compress bool, os opensearch.Client) (err error) {
	// Normalize query
	query = querydsl.NormalizeLuceneQuery(query)

	// Build query
	rangeDateQuery := querydsl.NewRangeQuery(dateField).
		Gte(fromDate).
		Lte(toDate)
	stringQuery := querydsl.NewQueryStringQuery(query).WithAnalyzeWildcard(true)
	boolQuery := querydsl.NewBoolQuery().Must(rangeDateQuery, stringQuery)

	// Forge payload
	computedFields := append(fields, splitFileColumn)
	pitResponse, err := os.Search().CreatePIT(
		ctx,
		&api.CreatePITRequest{
			Indices:   []string{index},
			KeepAlive: pitDuration,
		},
	)
	if err != nil {
		return errors.Wrap(err, "error to create PIT")
	}
	defer func() {
		_, err := os.Search().DeletePIT(ctx, &api.DeletePITRequest{
			PitIds: []string{pitResponse.PitId},
		})
		if err != nil {
			log.Errorf("error to delete PIT: %v", err)
		}
	}()

	reqDsl := querydsl.NewSearchRequest().
		Query(boolQuery).
		Size(querySize).
		Sort(dateField, true).
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
	cache := newFileWriterCache(compress)
	defer func() {
		if cerr := cache.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	log.Infof("Start search on Opensearch with index %s and search '%s", index, query)
	for {
		req, err := api.NewSearchRequest(reqDsl)
		if err != nil {
			return errors.Wrap(err, "error to create search request")
		}

		searchResult, err := os.Search().Search(ctx, req)
		if err != nil {
			return err
		}

		if len(searchResult.Hits.Hits) == 0 {
			break
		}

		if firstLoop {
			firstLoop = false
			log.Infof("Found %d document to export", searchResult.Hits.TotalHits.Value)
		}

		if err = processExport(searchResult, fields, separator, path, splitFileColumn, compress, cache); err != nil {
			return err
		}

		reqDsl.SearchAfter(searchResult.Hits.Hits[len(searchResult.Hits.Hits)-1].Sort...)
	}

	return nil
}

func exportDataToFilesWithClosedIndex(ctx context.Context, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, pitDuration string, compress bool, os opensearch.Client) error {
	fromDateExpr, err := datemath.Parse(fromDate)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", fromDate)
	}
	fromDateTime := fromDateExpr.Time()

	toDateExpr, err := datemath.Parse(toDate)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", toDate)
	}
	toDateTime := toDateExpr.Time()

	// Create metadata index
	if err = createMetadataindexIfNotExist(ctx, os); err != nil {
		return err
	}

	// Get current user
	authResponse, err := os.Security().AuthInfo(ctx)
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
	if err = createMetdata(ctx, metadata, os); err != nil {
		return errors.Wrap(err, "error to create metadata document")
	}

	defer func() {
		// Delete metadata document
		if err := cleanMetadataExportWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, os); err != nil {
			log.Errorf("error to delete metadata document %s: %v", metadata.Id, err)
		}
	}()

	// Register signal handler for graceful cleanup on Ctrl+C / kill
	sigCh := make(chan stdos.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Warn("Received termination signal, cleaning up...")
		// Flush buffered writers before exiting so no exported data is lost.
		flushAllWriterCaches()
		if cleanErr := cleanMetadataExportWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, os); cleanErr != nil {
			log.Errorf("Error during cleanup: %v", cleanErr)
		}
		stdos.Exit(1)
	}()
	defer signal.Stop(sigCh)

	log.Infof("Loop over index in datastream %s to found the starting index", index)

	fn := func(ctx context.Context, indexName string) error {
		return handleClosedIndex(ctx, metadata, querySize, fromDate, toDate, dateField, indexName, query, fields, separator, splitFileColumn, path, pitDuration, compress, os)
	}

	if err := forEachIndexInDateRange(ctx, os, index, fromDateTime, toDateTime, fn); err != nil {
		return err
	}

	return nil
}

// handleClosedIndex permit to open index if it's closed, process it and close it if it's not used by another session
func handleClosedIndex(ctx context.Context, metadata *Metadata, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, pitDuration string, compress bool, os opensearch.Client) (err error) {
	if _, err = lockIndex(ctx, index, metadata, os); err != nil {
		return errors.Wrapf(err, "error to lock index %s", index)
	}
	defer func() {
		// unlock index
		err = unlockIndex(ctx, index, metadata, os)
		if err != nil {
			log.Errorf("Error when unlock index %s: %s", index, err.Error())
		}
	}()

	return exportDataToFilesWithoutClosedIndex(ctx, querySize, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, pitDuration, compress, os)
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

func processExport(searchResult *querydsl.SearchResult, fields []string, separator string, path string, splitFileColumn string, compress bool, cache *fileWriterCache) (err error) {
	log.Debugf("Process %d documents", len(searchResult.Hits.Hits))

	// Loop over results
	if len(searchResult.Hits.Hits) > 0 {
		var fileName string

		for _, item := range searchResult.Hits.Hits {

			// Determine target file to write result
			jsonResult := gjson.ParseBytes(item.Source)
			if jsonResult.Get(splitFileColumn).Str == "" {
				log.Debugf("No value for field %s on document id %s", splitFileColumn, item.Id)
				fileName = fmt.Sprintf("%s/unknown_host", path)
			} else {
				safeValue := filepath.Base(jsonResult.Get(splitFileColumn).Str)
				fileName = fmt.Sprintf("%s/%s", path, safeValue)
			}
			if compress {
				fileName += ".gz"
			}

			writer, err := cache.get(fileName)
			if err != nil {
				return err
			}

			// Extract needed columns
			td := make([]string, 0)
			for _, field := range fields {
				td = append(td, jsonResult.Get(field).Str)
			}

			// Write result to the buffered writer
			_, err = fmt.Fprintf(writer, "%s\n", strings.Join(td, separator))
			if err != nil {
				log.Errorf("Error when write file: %s", err.Error())
				return err
			}
		}

	}

	return nil
}
