package opensearchtools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/disaster37/opensearch/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

type ESTestSuite struct {
	suite.Suite
	client opensearch.Client
}

func (s *ESTestSuite) SetupSuite() {
	// Init logger
	logrus.SetFormatter(new(prefixed.TextFormatter))
	logrus.SetLevel(logrus.DebugLevel)

	// Init client
	username := os.Getenv("OPENSEARCH_USERNAME")
	password := os.Getenv("OPENSEARCH_PASSWORD")

	cfg := &opensearch.Config{
		URL:           "https://opensearch.svc:9200",
		Username:      username,
		Password:      password,
		TLSSkipVerify: true,
	}

	client, err := opensearch.New(cfg, logrus.NewEntry(logrus.StandardLogger()))
	if err != nil {
		panic(err)
	}

	// Wait es is online
	isOnline := false
	for isOnline == false {
		if _, err = client.Cluster().Health(context.Background(), nil); err != nil {
			time.Sleep(5 * time.Second)
		} else {
			isOnline = true
		}
	}

	s.client = client
}

func (s *ESTestSuite) SetupTest() {
	// Do somethink before each test
}

func TestESTestSuite(t *testing.T) {
	suite.Run(t, new(ESTestSuite))
}

func (s *ESTestSuite) TestCheckConnexion() {
	err := checkConnexion(context.Background(), s.client)
	assert.NoError(s.T(), err)
}

func (s *ESTestSuite) TestCheckCluster() {
	clusterStatus, err := checkClusterStatus(context.Background(), s.client)
	assert.NoError(s.T(), err)
	assert.Regexp(s.T(), "green|yellow", clusterStatus)
}

func (s *ESTestSuite) TestClusterRoutingAllocation() {
	err := disableRoutingAllocation(context.Background(), s.client)
	assert.NoError(s.T(), err)

	err = enableRoutingAllocation(context.Background(), s.client)
	assert.NoError(s.T(), err)
}
