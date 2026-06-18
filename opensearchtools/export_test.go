package opensearchtools

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"

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
	err = exportDataToFiles(context.Background(), "now-1000y", "now", "@timestamp", "logs", "*", false, []string{"message"}, "|", "node_name", dir, "24h", false, s.client)
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
	err = exportDataToFiles(context.Background(), "now-1000y", "now", "@timestamp", "test", "*", true, []string{"message"}, "|", "node_name", dir, "24h", false, s.client)
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
	err = exportDataToFiles(context.Background(), "now-1000y", "now", "@timestamp", "logs", "*", false, []string{"message"}, "|", "node_name", dir, "24h", true, s.client)
	assert.NoError(s.T(), err)

	// Check output file exists as .gz and decompresses to expected content
	f, err := os.Open(fmt.Sprintf("%s/es-0.gz", dir))
	assert.NoError(s.T(), err)
	defer f.Close()

	gr, err := gzip.NewReader(f)
	assert.NoError(s.T(), err)
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n", string(decompressed))
}
