package opensearchtools

import (
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/disaster37/opensearch/v4/querydsl"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestProcessExport(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	sr := &querydsl.SearchResult{
		Hits: &querydsl.SearchHits{
			TotalHits: &querydsl.TotalHits{Value: 3, Relation: "eq"},
			Hits: []*querydsl.SearchHit{
				{Id: "1", Source: json.RawMessage(`{"message":"error A","node_name":"es-0"}`)},
				{Id: "2", Source: json.RawMessage(`{"message":"ok B","node_name":"es-1"}`)},
				{Id: "3", Source: json.RawMessage(`{"message":"error C","node_name":"es-0"}`)},
			},
		},
	}

	opts := exportOptions{
		fields:          []string{"message"},
		separator:       "|",
		splitFileColumn: "node_name",
		path:            dir,
		filters:         []*regexp.Regexp{regexp.MustCompile("error")},
	}

	cache := newFileWriterCache(false)
	defer func() { _ = cache.Close() }()

	written, err := processExport(sr, opts, cache)
	assert.NoError(t, err)
	assert.Equal(t, 2, written)

	// Writers are buffered; flush before inspecting the files on disk.
	cache.flush()

	content, err := os.ReadFile(dir + "/es-0")
	assert.NoError(t, err)
	assert.Equal(t, "error A\nerror C\n", string(content))

	_, err = os.ReadFile(dir + "/es-1")
	assert.True(t, os.IsNotExist(err))
}

func TestFormatExportSummary(t *testing.T) {
	assert.Equal(t, "Exported 0 documents after filtering (from 0 found)", formatExportSummary(0, 0))
	assert.Equal(t, "Exported 2 documents after filtering (from 3 found)", formatExportSummary(2, 3))
}

func TestFormatPerIndexSummary(t *testing.T) {
	assert.Equal(t, "Exported 2 documents after filtering (from 3 found) for index .ds-test-000001", formatPerIndexSummary(2, 3, ".ds-test-000001"))
}

func TestExportProgress(t *testing.T) {
	var p exportProgress
	assert.Equal(t, int64(0), p.found.Load())
	assert.Equal(t, int64(0), p.exported.Load())

	p.found.Add(3)
	p.exported.Add(2)
	assert.Equal(t, int64(3), p.found.Load())
	assert.Equal(t, int64(2), p.exported.Load())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				p.found.Add(1)
				p.exported.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = p.found.Load()
				_ = p.exported.Load()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(3+50*100), p.found.Load())
	assert.Equal(t, int64(2+50*100), p.exported.Load())
}

func TestLogInterruptSummary(t *testing.T) {
	var buf bytes.Buffer
	orig := log.StandardLogger().Out
	defer log.SetOutput(orig)
	log.SetOutput(&buf)

	logInterruptSummary(nil)
	assert.Contains(t, buf.String(), "Exported 0 documents after filtering (from 0 found) before interruption")

	buf.Reset()
	p := &exportProgress{}
	p.found.Add(5)
	p.exported.Add(2)
	logInterruptSummary(p)
	assert.Contains(t, buf.String(), "Exported 2 documents after filtering (from 5 found) before interruption")
}

func TestRunInterruptShutdownOrdering(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	var buf bytes.Buffer
	orig := log.StandardLogger().Out
	defer log.SetOutput(orig)
	log.SetOutput(&buf)

	cache := newFileWriterCache(false)
	defer func() { _ = cache.Close() }()

	fileName := dir + "/host"
	w, err := cache.get(fileName)
	assert.NoError(t, err)
	_, err = w.WriteString("buffered line\n")
	assert.NoError(t, err)

	p := &exportProgress{}
	p.found.Add(1)
	p.exported.Add(1)

	cleanupRan := false
	runInterruptShutdown(p, func() {
		cleanupRan = true
		log.Infof("cleanup done")
	})

	assert.True(t, cleanupRan)

	out := buf.String()
	summaryIdx := strings.Index(out, "before interruption")
	cleanupIdx := strings.Index(out, "cleanup done")
	assert.NotEqual(t, -1, summaryIdx)
	assert.NotEqual(t, -1, cleanupIdx)
	assert.Less(t, summaryIdx, cleanupIdx)

	content, err := os.ReadFile(fileName)
	assert.NoError(t, err)
	assert.Equal(t, "buffered line\n", string(content))
}

func TestValidateSeparator(t *testing.T) {
	for _, sep := range []string{";", ",", "|", "\t", "§"} {
		assert.NoError(t, validateSeparator(sep), "separator %q should be valid", sep)
	}

	for _, sep := range []string{"", ";;", "||", "\"", "\n", "\r", "\r\n", string([]byte{0xff})} {
		assert.Error(t, validateSeparator(sep), "separator %q should be invalid", sep)
	}
}

func TestFormatCSVLine(t *testing.T) {
	tests := []struct {
		name          string
		fields        []string
		sep           string
		want          string
		wantRoundTrip []string // nil means the original fields
	}{
		{name: "plain fields", fields: []string{"INFO", "main", "ok"}, sep: ";", want: "INFO;main;ok"},
		{name: "field with separator", fields: []string{"INFO", "a;b"}, sep: ";", want: `INFO;"a;b"`},
		{name: "field with double quotes", fields: []string{"INFO", `say "hello"`}, sep: ";", want: `INFO;"say ""hello"""`},
		{name: "field with LF", fields: []string{"ERROR", "line1\nline2"}, sep: ";", want: "ERROR;\"line1\nline2\""},
		{name: "field with CRLF", fields: []string{"ERROR", "line1\r\nline2"}, sep: ";", want: "ERROR;\"line1\r\nline2\"", wantRoundTrip: []string{"ERROR", "line1\nline2"}},
		{name: "field with CR only", fields: []string{"ERROR", "line1\rline2"}, sep: ";", want: "ERROR;\"line1\rline2\""},
		{name: "field with separator quotes and newline", fields: []string{"WARN", "a;\"b\"\nc"}, sep: ";", want: "WARN;\"a;\"\"b\"\"\nc\""},
		{name: "empty fields", fields: []string{"", "mid", ""}, sep: ";", want: ";mid;"},
		{name: "comma separator", fields: []string{"INFO", "a, b"}, sep: ",", want: `INFO,"a, b"`},
		{name: "pipe separator", fields: []string{"INFO", "a|b"}, sep: "|", want: `INFO|"a|b"`},
		{name: "single field", fields: []string{"a;b"}, sep: ";", want: `"a;b"`},
		{name: "no fields", fields: []string{}, sep: ";", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCSVLine(tt.fields, tt.sep)
			assert.Equal(t, tt.want, got)

			if len(tt.fields) == 0 {
				return
			}
			wantRT := tt.wantRoundTrip
			if wantRT == nil {
				wantRT = tt.fields
			}
			r := csv.NewReader(strings.NewReader(got))
			r.Comma = []rune(tt.sep)[0]
			record, err := r.Read()
			assert.NoError(t, err)
			assert.Equal(t, wantRT, record)
		})
	}
}

func TestProcessExportSingleFieldRawPreservesSpecialChars(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	raw := "a;b \"quoted\"\nline2\r\nline3"
	sr := &querydsl.SearchResult{
		Hits: &querydsl.SearchHits{
			TotalHits: &querydsl.TotalHits{Value: 1, Relation: "eq"},
			Hits: []*querydsl.SearchHit{
				{Id: "1", Source: json.RawMessage(`{"message":"a;b \"quoted\"\nline2\r\nline3","node_name":"es-0"}`)},
			},
		},
	}

	opts := exportOptions{
		fields:          []string{"message"},
		separator:       ";",
		splitFileColumn: "node_name",
		path:            dir,
	}

	cache := newFileWriterCache(false)
	defer func() { _ = cache.Close() }()

	written, err := processExport(sr, opts, cache)
	assert.NoError(t, err)
	assert.Equal(t, 1, written)
	cache.flush()

	content, err := os.ReadFile(dir + "/es-0")
	assert.NoError(t, err)
	assert.Equal(t, raw+"\n", string(content))
}

func TestProcessExportMultiFieldCSV(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	sr := &querydsl.SearchResult{
		Hits: &querydsl.SearchHits{
			TotalHits: &querydsl.TotalHits{Value: 7, Relation: "eq"},
			Hits: []*querydsl.SearchHit{
				{Id: "1", Source: json.RawMessage(`{"level":"INFO","component":"main","message":"started","node_name":"es-0"}`)},
				{Id: "2", Source: json.RawMessage(`{"level":"INFO","component":"main","message":"a;b","node_name":"es-0"}`)},
				{Id: "3", Source: json.RawMessage(`{"level":"INFO","component":"main","message":"say \"hello\"","node_name":"es-0"}`)},
				{Id: "4", Source: json.RawMessage(`{"level":"ERROR","component":"main","message":"line1\nline2","node_name":"es-0"}`)},
				{Id: "5", Source: json.RawMessage(`{"level":"ERROR","component":"main","message":"line1\r\nline2","node_name":"es-0"}`)},
				{Id: "6", Source: json.RawMessage(`{"level":"INFO","component":"main","node_name":"es-0"}`)},
				{Id: "7", Source: json.RawMessage(`{"level":"WARN","component":"main","message":"no host"}`)},
			},
		},
	}

	opts := exportOptions{
		fields:          []string{"level", "component", "message"},
		separator:       ";",
		splitFileColumn: "node_name",
		path:            dir,
	}

	cache := newFileWriterCache(false)
	defer func() { _ = cache.Close() }()

	written, err := processExport(sr, opts, cache)
	assert.NoError(t, err)
	assert.Equal(t, 7, written)
	cache.flush()

	content, err := os.ReadFile(dir + "/es-0")
	assert.NoError(t, err)
	assert.Equal(t, "INFO;main;started\n"+
		"INFO;main;\"a;b\"\n"+
		"INFO;main;\"say \"\"hello\"\"\"\n"+
		"ERROR;main;\"line1\nline2\"\n"+
		"ERROR;main;\"line1\r\nline2\"\n"+
		"INFO;main;\n", string(content))

	r := csv.NewReader(strings.NewReader(string(content)))
	r.Comma = ';'
	records, err := r.ReadAll()
	assert.NoError(t, err)
	assert.Equal(t, [][]string{
		{"INFO", "main", "started"},
		{"INFO", "main", "a;b"},
		{"INFO", "main", `say "hello"`},
		{"ERROR", "main", "line1\nline2"},
		{"ERROR", "main", "line1\nline2"},
		{"INFO", "main", ""},
	}, records)

	content, err = os.ReadFile(dir + "/unknown_host")
	assert.NoError(t, err)
	assert.Equal(t, "WARN;main;no host\n", string(content))

	t.Run("filter matches raw joined value", func(t *testing.T) {
		dir2, err := os.MkdirTemp("/tmp", "test")
		assert.NoError(t, err)
		defer func() { _ = os.RemoveAll(dir2) }()

		opts2 := opts
		opts2.path = dir2
		opts2.filters = []*regexp.Regexp{regexp.MustCompile(`INFO;main;say "hello"`)}

		cache2 := newFileWriterCache(false)
		defer func() { _ = cache2.Close() }()

		written, err := processExport(sr, opts2, cache2)
		assert.NoError(t, err)
		assert.Equal(t, 1, written)
		cache2.flush()

		content, err := os.ReadFile(dir2 + "/es-0")
		assert.NoError(t, err)
		assert.Equal(t, "INFO;main;\"say \"\"hello\"\"\"\n", string(content))
	})
}

func TestProcessExportMultiFieldCSVCompressed(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	sr := &querydsl.SearchResult{
		Hits: &querydsl.SearchHits{
			TotalHits: &querydsl.TotalHits{Value: 1, Relation: "eq"},
			Hits: []*querydsl.SearchHit{
				{Id: "1", Source: json.RawMessage(`{"level":"INFO","message":"a;b","node_name":"es-0"}`)},
			},
		},
	}

	opts := exportOptions{
		fields:          []string{"level", "message"},
		separator:       ";",
		splitFileColumn: "node_name",
		path:            dir,
		compress:        true,
	}

	cache := newFileWriterCache(true)
	written, err := processExport(sr, opts, cache)
	assert.NoError(t, err)
	assert.Equal(t, 1, written)
	assert.NoError(t, cache.Close())

	f, err := os.Open(dir + "/es-0.gz")
	assert.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()

	gr, err := gzip.NewReader(f)
	assert.NoError(t, err)
	defer func() { assert.NoError(t, gr.Close()) }()

	decompressed, err := io.ReadAll(gr)
	assert.NoError(t, err)
	assert.Equal(t, "INFO;\"a;b\"\n", string(decompressed))
}
