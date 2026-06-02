package opensearchtools

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/disaster37/opensearch/v4"
	"github.com/disaster37/opensearch/v4/api"
	"github.com/disaster37/opensearch/v4/querydsl"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
}

//go:embed files/template_opensearchtools.json
var metadataIndexTemplate string

// createMetadataindexIfNotExist with create index dedicated for metadata
func createMetadataindexIfNotExist(ctx context.Context, os opensearch.Client) (err error) {

	isIndexExist, err := os.Indices().Exists(ctx, []string{metadataIndexName})
	if err != nil {
		return errors.Wrapf(err, "error to check if index %s exist", metadataIndexName)
	}

	if !isIndexExist {
		createIndexResponse, err := os.Indices().Create(ctx, metadataIndexName, metadataIndexTemplate)
		if err != nil {
			return errors.Wrapf(err, "error to create index %s", metadataIndexName)
		}
		if !createIndexResponse.Acknowledged {
			return errors.Errorf("error to create index %s", metadataIndexName)
		}
	}

	return nil

}

// cleanMetadataExportWithAutoOpenIndex clean metadata export with auto open index
func cleanMetadataExportWithAutoOpenIndex(ctx context.Context, user string, force bool, os opensearch.Client) (err error) {
	// Search if metadata exist
	query := querydsl.NewBoolQuery().
		Must(querydsl.NewTermQuery("type", MetadataTypeExportAutoOpenIndex))

	if user != "" {
		query = query.Must(querydsl.NewTermQuery("user", user))
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

	if searchResponse.Hits.TotalHits.Value > 0 {
		// Loop over data to close opened index
		for _, doc := range searchResponse.Hits.Hits {
			metadata := new(Metadata)
			err = json.Unmarshal(doc.Source, &metadata)
			if err != nil {
				return errors.Wrapf(err, "error to unmarshal metadata export")
			}

			// Close index
			for _, index := range metadata.Indexes {

				if !force {
					canBeClosed, err := isIndexCanBeClosed(ctx, index, os)
					if err != nil {
						return errors.Wrapf(err, "error to check if index %s can be closed", index)
					}

					if !canBeClosed {
						logrus.Infof("Index %s is already used by another session, skip close it", index)
					}
				}

				if resp, err := os.Indices().Close(ctx, index); err != nil || !resp.Acknowledged {
					return errors.Wrapf(err, "error to close index %s", index)
				}
				logrus.Infof("Close index %s", index)
			}

			// Delete metadata
			_, err = os.Document().Delete(ctx, &api.DeleteRequest{Index: metadataIndexName, Id: doc.Id})
			if err != nil {
				return errors.Wrapf(err, "error to delete metadata export with id %s", doc.Id)
			}
		}
	}

	return nil
}

func isIndexCanBeClosed(ctx context.Context, indexName string, os opensearch.Client) (bool, error) {
	query := querydsl.NewTermQuery("indexes", indexName)

	req, err := api.NewSearchRequest(querydsl.NewSearchRequest().
		Index(metadataIndexName).
		Query(query).
		Size(10000),
	)
	if err != nil {
		return false, err
	}

	res, err := os.Search().Search(ctx, req)
	if err != nil {
		return false, err
	}

	if res.Hits.TotalHits.Value > 1 {
		return false, nil
	}

	return true, nil
}
