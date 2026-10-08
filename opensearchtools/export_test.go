package opensearchtools

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func (s *ESTestSuite) TestExportDataToFiles() {
	logrus.SetLevel(logrus.TraceLevel)

	dir, err := os.MkdirTemp("/tmp", "test")
	if err != nil {
		s.T().Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Exports data without errors
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
	assert.NoError(s.T(), err)

	// Check output file exists
	content, err := os.ReadFile(fmt.Sprintf("%s/es-0", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n", string(content))

	content, err = os.ReadFile(fmt.Sprintf("%s/es-1", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868264] overhead, spent [279ms] collecting in the last [1s]\n", string(content))
}

func (s *ESTestSuite) TestExportDataToFilesWithOpenIndex() {
	logrus.SetLevel(logrus.TraceLevel)

	dir, err := os.MkdirTemp("/tmp", "test")
	if err != nil {
		s.T().Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Exports data without errors
	opts := exportOptions{
		fromDate:          "now-1000y",
		toDate:            "now",
		dateField:         "@timestamp",
		index:             "test",
		query:             "*",
		isOpenClosedIndex: true,
		fields:            []string{"message"},
		separator:         "|",
		splitFileColumn:   "node_name",
		path:              dir,
		pitDuration:       "24h",
		compress:          false,
		os:                s.client,
	}
	err = exportDataToFiles(context.Background(), opts)
	assert.NoError(s.T(), err)

	// Check output file exists
	content, err := os.ReadFile(fmt.Sprintf("%s/es-0", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n", string(content))

	content, err = os.ReadFile(fmt.Sprintf("%s/es-1", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868264] overhead, spent [279ms] collecting in the last [1s]\n", string(content))
}

func (s *ESTestSuite) TestExportDataToFilesCompressed() {
	logrus.SetLevel(logrus.TraceLevel)

	dir, err := os.MkdirTemp("/tmp", "test")
	if err != nil {
		s.T().Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Exports data with compression enabled
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
		compress:          true,
		os:                s.client,
	}
	err = exportDataToFiles(context.Background(), opts)
	assert.NoError(s.T(), err)

	// Check output file exists as .gz and decompresses to expected content
	f, err := os.Open(fmt.Sprintf("%s/es-0.gz", dir))
	assert.NoError(s.T(), err)
	defer func() { assert.NoError(s.T(), f.Close()) }()

	gr, err := gzip.NewReader(f)
	assert.NoError(s.T(), err)
	defer func() { assert.NoError(s.T(), gr.Close()) }()

	decompressed, err := io.ReadAll(gr)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n", string(decompressed))
}

func (s *ESTestSuite) TestExportDataToFilesWithFilters() {
	logrus.SetLevel(logrus.TraceLevel)

	dir, err := os.MkdirTemp("/tmp", "test")
	if err != nil {
		s.T().Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

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
		filters:           []*regexp.Regexp{regexp.MustCompile("334ms")},
		os:                s.client,
	}

	stats, err := exportDataToFilesWithoutClosedIndex(context.Background(), opts)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), int64(2), stats.found)
	assert.Equal(s.T(), int64(1), stats.exported)

	content, err := os.ReadFile(fmt.Sprintf("%s/es-0", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n", string(content))

	_, err = os.ReadFile(fmt.Sprintf("%s/es-1", dir))
	assert.True(s.T(), os.IsNotExist(err))
}
