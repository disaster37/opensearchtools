package opensearchtools

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/disaster37/opensearch/v2"
	"github.com/disaster37/opensearch/v2/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
	"k8s.io/utils/ptr"
)

type ESTestSuite struct {
	suite.Suite
	client *opensearch.Client
}

func (s *ESTestSuite) SetupSuite() {

	// Init logger
	logrus.SetFormatter(new(prefixed.TextFormatter))
	logrus.SetLevel(logrus.DebugLevel)

	// Init client
	urls := strings.Split(os.Getenv("OPENSEARCH_URLS"), ",")
	username := os.Getenv("OPENSEARCH_USERNAME")
	password := os.Getenv("OPENSEARCH_PASSWORD")

	cfg := &config.Config{
		URLs:        urls,
		Username:    username,
		Password:    password,
		Sniff:       ptr.To[bool](false),
		Healthcheck: ptr.To[bool](false),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	client, err := opensearch.NewClientFromConfig(cfg)
	if err != nil {
		panic(err)
	}

	// Wait es is online
	isOnline := false
	for isOnline == false {
		if _, err = client.ClusterHealth().Do(context.Background()); err != nil {
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

	err := checkConnexion(s.client)
	assert.NoError(s.T(), err)
}

func (s *ESTestSuite) TestCheckCluster() {

	clusterStatus, err := checkClusterStatus(s.client)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "green", clusterStatus)
}

func (s *ESTestSuite) TestClusterRoutingAllocation() {

	err := disableRoutingAllocation(s.client)
	assert.NoError(s.T(), err)

	err = enableRoutingAllocation(s.client)
	assert.NoError(s.T(), err)
}
