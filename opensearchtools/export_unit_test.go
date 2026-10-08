package opensearchtools

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/disaster37/opensearch/v4/querydsl"
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
