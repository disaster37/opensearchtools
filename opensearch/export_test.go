package opensearchtools

import (
	"fmt"
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
	defer os.RemoveAll(dir)

	// Exports data without errors
	err = exportDataToFiles("now-1000y", "now", "timestamp", "logs", "*", []string{"message"}, "|", "node_name", dir, s.client)
	assert.NoError(s.T(), err)

	// Check output file exists
	content, err := os.ReadFile(fmt.Sprintf("%s/es-0", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868238] overhead, spent [334ms] collecting in the last [1s]\n", string(content))

	content, err = os.ReadFile(fmt.Sprintf("%s/es-1", dir))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "[gc][17868264] overhead, spent [279ms] collecting in the last [1s]\n", string(content))

}
