package opensearchtools

import (
	"bytes"
	"encoding/json"
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
