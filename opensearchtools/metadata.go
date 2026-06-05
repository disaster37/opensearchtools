package opensearchtools

import (
	"context"
	_ "embed"

	gojson "github.com/goccy/go-json"

	"github.com/disaster37/opensearch/v4"
	"github.com/disaster37/opensearch/v4/api"
	"github.com/disaster37/opensearch/v4/querydsl"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"github.com/urfave/cli/v2"
)

const (
	metadataIndexName               = ".opensearchtools"
	MetadataTypeExportAutoOpenIndex = "export_data"
)

// Metadata is the struct to store metadata in opensearch
type Metadata struct {
	Id        string   `json:"-"`
	Type      string   `json:"type"`
	User      string   `json:"user"`
	SessionId string   `json:"sessionId"`
	Indexes   []string `json:"indexes"`
	Trigger   string   `json:"trigger,omitempty"`
}

//go:embed files/template_opensearchtools.json
var metadataIndexTemplate string

// CleanMetata permit to clean metadata and close index that need to be
func CleanMetata(c *cli.Context) error {
	os, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	// Check use have role admin, it's admin task
	authResp, err := os.Security().AuthInfo(c.Context)
	if err != nil {
		return errors.Wrap(err, "error to get auth info")
	}

	if !funk.ContainsString(authResp.Roles, "all_access") {
		return errors.New("you must be admin to clean metadata")
	}

	return cleanMetadataExportWithAutoOpenIndex(c.Context, "", "", os)

}

// createMetadataindexIfNotExist with create index dedicated for metadata
func createMetadataindexIfNotExist(ctx context.Context, os opensearch.Client) (err error) {

	isIndexExist, err := os.Indices().Exists(ctx, []string{metadataIndexName})
	if err != nil {
		return errors.Wrapf(err, "error to check if index %s exist", metadataIndexName)
	}

	if !isIndexExist {
		createIndexResponse, err := os.Indices().Create(ctx, metadataIndexName, metadataIndexTemplate)
		if err != nil || !createIndexResponse.Acknowledged {
			return errors.Wrapf(err, "error to create index %s", metadataIndexName)
		}
	}

	return nil

}

// createMetdata permit to create metadata document
func createMetdata(ctx context.Context, metadata *Metadata, os opensearch.Client) (err error) {
	doc, err := os.Document().Index(ctx, &api.IndexRequest{Index: metadataIndexName, Body: metadata, Params: &api.IndexParams{Refresh: api.RefreshTrue}})
	if err != nil {
		return errors.Wrapf(err, "error to update metadata %s", metadata.Id)
	}

	metadata.Id = doc.Id

	return nil
}

// lockIndex permit to open index if is closed
// If index is closed, it track it on metadata
// If index is open and already track on metadata, we add it on metadata to not close it by anoter processes
func lockIndex(ctx context.Context, index string, metadata *Metadata, os opensearch.Client) (err error) {
	var (
		isClosed      bool
		isAlreadyOpen bool
	)

	if metadata.Id == "" {
		return errors.New("metadata id is empty")
	}

	// Check if index is closed
	indexState, err := os.Cat().Indices(ctx, []string{index})
	if err != nil {
		return errors.Wrapf(err, "error to get index %s", index)
	}

	if len(indexState) == 0 {
		return errors.Errorf("index %s not found", index)
	}

	logrus.Debugf("Index state: %+v", indexState)

	if indexState[0].Status == "close" {
		isClosed = true
	} else {
		// search if is normally closed index but already openned by another process
		count, err := countIndexInMetdata(ctx, index, os)
		if err != nil {
			return errors.Wrapf(err, "error to check if index %s can be closed", index)
		}

		if count > 0 {
			isAlreadyOpen = true
		}
	}

	// Add metadata if index is closed or already open by another process
	if isClosed || isAlreadyOpen {
		metadata.Indexes = append(metadata.Indexes, index)
		if _, err := os.Document().Index(ctx, &api.IndexRequest{Index: metadataIndexName, Id: metadata.Id, Body: metadata, Params: &api.IndexParams{Refresh: api.RefreshTrue}}); err != nil {
			return errors.Wrapf(err, "error to update metadata %s", metadata.Id)
		}
	}

	// If index is closed, open it
	if isClosed {
		if resp, err := os.Indices().Open(ctx, index); err != nil || !resp.Acknowledged {
			return errors.Wrapf(err, "error to open index %s", index)
		}
		logrus.Infof("Open index %s", index)
	}

	return nil
}

// unlockIndex permit to close index if it's not used by another session
// The index must be present on metadata, else is regular index than can be closed
func unlockIndex(ctx context.Context, index string, metadata *Metadata, os opensearch.Client) (err error) {

	if metadata.Id == "" {
		return errors.New("metadata id is empty")
	}

	count, err := countIndexInMetdata(ctx, index, os)
	if err != nil {
		return errors.Wrapf(err, "error to check if index %s can be closed", index)
	}

	if count > 1 {
		logrus.Infof("Index %s is already used by another session, skip close it", index)
	}

	if count == 1 {
		// Close index
		if resp, err := os.Indices().Close(ctx, index); err != nil || !resp.Acknowledged {
			return errors.Wrapf(err, "error to close index %s", index)
		}
		logrus.Infof("Close index %s", index)
	}

	// Remove index from metadata
	for i, idx := range metadata.Indexes {
		if idx == index {
			metadata.Indexes = append(metadata.Indexes[:i], metadata.Indexes[i+1:]...)
			break
		}
	}

	if _, err = os.Document().Index(ctx, &api.IndexRequest{Index: metadataIndexName, Id: metadata.Id, Body: metadata, Params: &api.IndexParams{Refresh: api.RefreshTrue}}); err != nil {
		return errors.Wrapf(err, "error to update metadata %s", metadata.Id)
	}

	return nil
}

// cleanMetadataExportWithAutoOpenIndex clean metadata export with auto open index
func cleanMetadataExportWithAutoOpenIndex(ctx context.Context, user string, sessionId string, os opensearch.Client) (err error) {
	// Search for metadata documents of export type (and optional user filter)
	query := querydsl.NewBoolQuery().
		Must(querydsl.NewTermQuery("type", MetadataTypeExportAutoOpenIndex))

	if user != "" {
		query = query.Must(querydsl.NewTermQuery("user", user))
	}

	if sessionId != "" {
		query = query.Must(querydsl.NewTermQuery("sessionId", sessionId))
	}

	req, err := api.NewSearchRequest(
		querydsl.NewSearchRequest().
			Index(metadataIndexName).
			Query(query).
			Size(10000),
	)
	if err != nil {
		return err
	}

	searchResponse, err := os.Search().Search(ctx, req)
	if err != nil {
		return errors.Wrapf(err, "error to search metadata export")
	}

	// Process each metadata document independently
	for _, doc := range searchResponse.Hits.Hits {
		metadata := new(Metadata)
		logrus.Debugf("Doc source %s", string(doc.Source))
		if err = gojson.Unmarshal(doc.Source, metadata); err != nil {
			return errors.Wrapf(err, "error to unmarshal metadata export")
		}

		// Close each index referenced in this metadata document
		for _, index := range metadata.Indexes {
			logrus.Debugf("Cleaning up index %s for metadata %s", index, metadata.Id)
			unlockIndex(ctx, index, metadata, os)
		}

		// Delete the metadata document after its indexes are processed
		if _, err = os.Document().Delete(ctx, &api.DeleteRequest{Index: metadataIndexName, Id: doc.Id, Params: &api.DeleteParams{Refresh: api.RefreshTrue}}); err != nil {
			return errors.Wrapf(err, "error to delete metadata export with id %s", doc.Id)
		}
	}
	return nil
}

// countIndexInMetdata count index in metadata
func countIndexInMetdata(ctx context.Context, indexName string, os opensearch.Client) (int, error) {
	query := querydsl.NewTermQuery("indexes", indexName)

	req, err := api.NewSearchRequest(querydsl.NewSearchRequest().
		Index(metadataIndexName).
		Query(query),
	//Size(1000). // Don't return documents, only count
	//TrackTotalHits(true),
	)
	if err != nil {
		return 0, err
	}

	res, err := os.Search().Search(ctx, req)
	if err != nil {
		return 0, err
	}

	return int(res.Hits.TotalHits.Value), nil
}
