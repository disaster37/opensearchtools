package opensearchtools

import (
	"context"

	"github.com/disaster37/opensearch/v4"
	"github.com/stretchr/testify/assert"
)

func (s *ESTestSuite) TestCleanMetadataExportWithAutoOpenIndex() {
	// Skip test if client is not available
	if s.client == nil {
		s.FailNow("OpenSearch client not available")
	}

	// Delete metadata index
	_, err := s.client.Indices().Delete(context.Background(), []string{metadataIndexName})
	if err != nil && !opensearch.IsNotFound(err) {
		s.FailNow(err.Error())
	}

	// Create metadata index
	err = createMetadataindexIfNotExist(context.Background(), s.client)
	s.NoError(err)
	err = createMetadataindexIfNotExist(context.Background(), s.client)
	s.NoError(err)

	// Check mapping
	mapping, err := s.client.Indices().GetMapping(context.Background(), []string{metadataIndexName})
	s.NoError(err)
	assert.Equal(s.T(), "date", mapping[metadataIndexName].(map[string]any)["mappings"].(map[string]any)["properties"].(map[string]any)["@timestamp"].(map[string]any)["type"].(string))
	assert.Equal(s.T(), "keyword", mapping[metadataIndexName].(map[string]any)["mappings"].(map[string]any)["properties"].(map[string]any)["user"].(map[string]any)["type"].(string))
	assert.Equal(s.T(), "keyword", mapping[metadataIndexName].(map[string]any)["mappings"].(map[string]any)["properties"].(map[string]any)["indexes"].(map[string]any)["type"].(string))
	assert.Equal(s.T(), "keyword", mapping[metadataIndexName].(map[string]any)["mappings"].(map[string]any)["properties"].(map[string]any)["type"].(map[string]any)["type"].(string))

	// Delete datastream index
	_, err = s.client.Indices().DeleteDataStream(context.Background(), []string{"test-metadata"})
	if err != nil && !opensearch.IsNotFound(err) {
		s.FailNow(err.Error())
	}

	// Create datastream index
	_, err = s.client.Indices().CreateDataStream(context.Background(), "test-metadata")
	if err != nil {
		s.FailNow(err.Error())
	}

	// Rollover the datastream index to have more then one index
	_, err = s.client.Indices().Rollover(context.Background(), "test-metadata", nil)
	if err != nil {
		s.FailNow(err.Error())
	}

	datastreamIndex, err := s.client.Indices().GetDataStream(context.Background(), []string{"test-metadata"})
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

	// Create metadata document referencing both indexes
	meta1 := &Metadata{
		Type:      MetadataTypeExportAutoOpenIndex,
		User:      "tester",
		SessionId: "sess1",
		Indexes:   []string{},
	}
	err = createMetdata(context.Background(), meta1, s.client)
	s.NoError(err)
	s.NotEmpty(meta1.Id)

	// lock indexes
	isOpenIndex, err := lockIndex(context.Background(), idx1, meta1, s.client)
	s.NoError(err)
	s.True(isOpenIndex)
	isOpenIndex, err = lockIndex(context.Background(), idx2, meta1, s.client)
	s.NoError(err)
	s.False(isOpenIndex)

	count, err := countIndexInMetdata(context.Background(), idx1, s.client)
	s.NoError(err)
	s.Equal(1, count)
	count, err = countIndexInMetdata(context.Background(), idx2, s.client)
	s.NoError(err)
	s.Equal(0, count)

	// unlock indexes
	err = unlockIndex(context.Background(), idx1, meta1, s.client)
	s.NoError(err)
	err = unlockIndex(context.Background(), idx2, meta1, s.client)
	s.NoError(err)

	count, err = countIndexInMetdata(context.Background(), idx1, s.client)
	s.NoError(err)
	s.Equal(0, count)
	count, err = countIndexInMetdata(context.Background(), idx2, s.client)
	s.NoError(err)
	s.Equal(0, count)

	// Check index 1 is closed
	catResult, err := s.client.Cat().Indices(context.Background(), []string{idx1})
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "close", catResult[0].Status)

	// Check index 2 is open
	catResult, err = s.client.Cat().Indices(context.Background(), []string{idx2})
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "open", catResult[0].Status)

	// Create second metadata referencing idx1 only to simulate another session
	meta2 := &Metadata{
		Type:      MetadataTypeExportAutoOpenIndex,
		User:      "tester",
		SessionId: "sess2",
		Indexes:   []string{},
	}
	err = createMetdata(context.Background(), meta2, s.client)
	s.NoError(err)
	s.NotEmpty(meta2.Id)

	// lock indexes
	isOpenIndex, err = lockIndex(context.Background(), idx1, meta1, s.client)
	s.NoError(err)
	s.True(isOpenIndex)
	isOpenIndex, err = lockIndex(context.Background(), idx2, meta1, s.client)
	s.NoError(err)
	s.False(isOpenIndex)
	isOpenIndex, err = lockIndex(context.Background(), idx1, meta2, s.client)
	s.NoError(err)
	s.True(isOpenIndex)
	isOpenIndex, err = lockIndex(context.Background(), idx2, meta2, s.client)
	s.NoError(err)
	s.False(isOpenIndex)

	count, err = countIndexInMetdata(context.Background(), idx1, s.client)
	s.NoError(err)
	s.Equal(2, count)
	count, err = countIndexInMetdata(context.Background(), idx2, s.client)
	s.NoError(err)
	s.Equal(0, count)

	// unlock indexes for first session
	err = unlockIndex(context.Background(), idx1, meta1, s.client)
	s.NoError(err)
	err = unlockIndex(context.Background(), idx2, meta1, s.client)
	s.NoError(err)

	count, err = countIndexInMetdata(context.Background(), idx1, s.client)
	s.NoError(err)
	s.Equal(1, count)
	count, err = countIndexInMetdata(context.Background(), idx2, s.client)
	s.NoError(err)
	s.Equal(0, count)

	// Check index 1 is open
	catResult, err = s.client.Cat().Indices(context.Background(), []string{idx1})
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "open", catResult[0].Status)

	// Check index 2 is open
	catResult, err = s.client.Cat().Indices(context.Background(), []string{idx2})
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "open", catResult[0].Status)

	// unlock indexes for second session
	err = unlockIndex(context.Background(), idx1, meta1, s.client)
	s.NoError(err)
	err = unlockIndex(context.Background(), idx2, meta1, s.client)
	s.NoError(err)

	// Check index 1 is closed
	catResult, err = s.client.Cat().Indices(context.Background(), []string{idx1})
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "close", catResult[0].Status)

	// Check index 2 is open
	catResult, err = s.client.Cat().Indices(context.Background(), []string{idx2})
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "open", catResult[0].Status)
}
