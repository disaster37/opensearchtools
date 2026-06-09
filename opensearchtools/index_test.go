package opensearchtools

import (
	"context"

	"github.com/disaster37/opensearch/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func (s *ESTestSuite) TestOpenClosedIndex() {
	logrus.SetLevel(logrus.TraceLevel)

	// Override prompt to auto-confirm and track calls
	origPrompt := promptConfirmFunc
	defer func() { promptConfirmFunc = origPrompt }()

	var promptCalls []string
	promptConfirmFunc = func(label string) bool {
		promptCalls = append(promptCalls, label)
		return true
	}

	// Delete datastream index
	_, err := s.client.Indices().DeleteDataStream(context.Background(), []string{"test-explore"})
	if err != nil && !opensearch.IsNotFound(err) {
		s.FailNow(err.Error())
	}

	// Create datastream index
	_, err = s.client.Indices().CreateDataStream(context.Background(), "test-explore")
	if err != nil {
		s.FailNow(err.Error())
	}

	// Rollover the datastream index to have more then one index
	_, err = s.client.Indices().Rollover(context.Background(), "test-explore", nil)
	if err != nil {
		s.FailNow(err.Error())
	}

	datastreamIndex, err := s.client.Indices().GetDataStream(context.Background(), []string{"test-explore"})
	if err != nil {
		s.FailNow(err.Error())
	}
	idx1 := datastreamIndex.DataStreams[0].Indices[0].IndexName
	idx2 := datastreamIndex.DataStreams[0].Indices[1].IndexName

	// Closed the first index
	_, err = s.client.Indices().Close(context.Background(), idx1)
	if err != nil {
		s.FailNow(err.Error())
	}

	err = openClosedIndex(context.Background(), "now-1d", "now", "test-explore", 10, s.client)
	s.NoError(err)

	// Verify prompt was called exactly once (the final "Close all indexes?" prompt)
	s.Len(promptCalls, 1)
	s.Contains(promptCalls[0], "Close all indexes")

	// Verify idx1 is now open (was closed before)
	catResp, err := s.client.Cat().Indices(context.Background(), []string{idx1})
	s.NoError(err)
	s.Require().NotEmpty(catResp)
	assert.Equal(s.T(), "close", catResp[0].Status)

	// Verify idx2 is still open
	catResp2, err := s.client.Cat().Indices(context.Background(), []string{idx2})
	s.NoError(err)
	s.Require().NotEmpty(catResp2)
	assert.Equal(s.T(), "open", catResp2[0].Status)
}
