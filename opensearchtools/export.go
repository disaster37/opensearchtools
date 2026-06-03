package opensearchtools

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/disaster37/opensearch/v4"
	"github.com/disaster37/opensearch/v4/api"
	"github.com/disaster37/opensearch/v4/querydsl"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/timberio/go-datemath"
	"github.com/urfave/cli/v2"
)

// ExportDataToFiles permit to extract some datas to files
// It return error if something wrong
func ExportDataToFiles(c *cli.Context) error {
	os, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	from := c.String("from")
	to := c.String("to")
	dateField := c.String("date-field")
	index := c.String("index")
	query := c.String("query")
	fields := c.StringSlice("fields")
	separator := c.String("separator")
	splitFileField := c.String("split-file-field")
	path := c.String("path")
	isOpenClosedIndex := c.Bool("open-index")
	pitDuraction := c.String("pit-duration")

	if path == "" {
		return errors.New("You must set --path")
	}
	if splitFileField == "" {
		return errors.New("You must set --split-file-field")
	}
	if query == "" {
		return errors.New("You must set --query")
	}

	err = exportDataToFiles(c.Context, from, to, dateField, index, query, isOpenClosedIndex, fields, separator, splitFileField, path, pitDuraction, os)
	if err != nil {
		return err
	}

	log.Infof("Extract successfully")

	return nil
}

func exportDataToFiles(ctx context.Context, fromDate string, toDate string, dateField string, index string, query string, isOpenClosedIndex bool, fields []string, separator string, splitFileColumn string, path string, pitDuration string, os opensearch.Client) error {
	if path == "" {
		return errors.New("You must provide path")
	}
	if index == "" {
		return errors.New("You must provide index")
	}

	if dateField == "" {
		return errors.New("You must provide date-field")
	}

	if os == nil {
		return errors.New("You must provide es client")
	}

	size := 10000

	log.Debugf("fromDate: %s", fromDate)
	log.Debugf("toDate: %s", toDate)
	log.Debugf("dateField: %s", dateField)
	log.Debugf("index: %s", index)
	log.Debugf("query: %s", query)
	log.Debugf("fields: %s", fields)
	log.Debugf("separator: %s", separator)
	log.Debugf("splitFileColumn: %s", splitFileColumn)
	log.Debugf("path: %s", path)
	log.Debugf("isOpenClosedIndex: %t", isOpenClosedIndex)
	log.Debugf("pitDuration: %s", pitDuration)

	// Search on all index, without needed to work index by index
	if !isOpenClosedIndex {
		return exportDataToFilesWithoutClosedIndex(ctx, size, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, pitDuration, os)
	} else {
		// Work index by index
		return exportDataToFilesWithClosedIndex(ctx, size, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, pitDuration, os)
	}
}

func exportDataToFilesWithoutClosedIndex(ctx context.Context, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, pitDuration string, os opensearch.Client) error {

	// Build query
	rangeDateQuery := querydsl.NewRangeQuery(dateField).
		Gte(fromDate).
		Lte(toDate)
	stringQuery := querydsl.NewQueryStringQuery(query).WithAnalyzeWildcard(true)
	boolQuery := querydsl.NewBoolQuery().Must(rangeDateQuery, stringQuery)

	// Forge payload
	computedFields := append(fields, splitFileColumn)
	pitResponse, err := os.Search().CreatePIT(
		ctx,
		&api.CreatePITRequest{
			Indices:   []string{index},
			KeepAlive: pitDuration,
		},
	)
	if err != nil {
		return errors.Wrap(err, "error to create PIT")
	}
	defer func() {
		_, err := os.Search().DeletePIT(ctx, &api.DeletePITRequest{
			PitIds: []string{pitResponse.PitId},
		})
		if err != nil {
			log.Errorf("error to delete PIT: %v", err)
		}
	}()

	reqDsl := querydsl.NewSearchRequest().
		Query(boolQuery).
		Size(querySize).
		Sort(dateField, true).
		Sort("_shard_doc", true).
		FetchSourceContext(querydsl.NewFetchSourceContext(true).Include(computedFields...)).
		TrackTotalHits(true).
		PointInTime(querydsl.NewPointInTime(pitResponse.PitId))

	// Get records over scroll
	firstLoop := true
	for {
		req, err := api.NewSearchRequest(reqDsl)
		if err != nil {
			return errors.Wrap(err, "error to create search request")
		}

		searchResult, err := os.Search().Search(ctx, req)
		if err != nil {
			return err
		}

		if len(searchResult.Hits.Hits) == 0 {
			break
		}

		if firstLoop {
			firstLoop = false
			log.Infof("Found %d document to export", searchResult.Hits.TotalHits.Value)
		}

		if err = processExport(searchResult, fields, separator, path, splitFileColumn); err != nil {
			return err
		}

		reqDsl.SearchAfter(searchResult.Hits.Hits[len(searchResult.Hits.Hits)-1].Sort...)
	}

	return nil

}

func exportDataToFilesWithClosedIndex(ctx context.Context, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, pitDuration string, os opensearch.Client) error {

	// Check if data stream index first
	datastreamIndexResponse, err := os.Indices().GetDataStream(ctx, []string{index})
	if err != nil {
		return errors.Errorf("The index %s is not a data stream index. You can't use 'open-index' parameter", index)
	}

	fromDateExpr, err := datemath.Parse(fromDate)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", fromDate)
	}
	fromDateTime := fromDateExpr.Time()

	toDateExpr, err := datemath.Parse(toDate)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", toDate)
	}
	toDateTime := toDateExpr.Time()

	var creationDate time.Time
	isFoundStartingIndex := false

	// Create metadata index
	if err = createMetadataindexIfNotExist(ctx, os); err != nil {
		return err
	}

	// Get current user
	authResponse, err := os.Security().AuthInfo(ctx)
	if err != nil {
		return errors.Wrap(err, "error to get current user")
	}

	// Create metadata document
	metadata := &Metadata{
		Type:      MetadataTypeExportAutoOpenIndex,
		User:      authResponse.UserName,
		SessionId: uuid.New().String(),
		Indexes:   []string{},
	}
	indexResponse, err := os.Document().Index(ctx, &api.IndexRequest{Index: metadataIndexName, Body: metadata})
	if err != nil {
		return errors.Wrap(err, "error to create metadata document")
	}
	metadata.Id = indexResponse.Id

	defer func() {
		// Delete metadata document
		if _, err := os.Document().Delete(ctx, &api.DeleteRequest{Index: metadataIndexName, Id: metadata.Id}); err != nil {
			logrus.Errorf("error to delete metadata document %s: %v", metadata.Id, err)
		}
	}()

	// Loop over index and search the index creation time that match the date range
	for _, datastreamIndex := range datastreamIndexResponse.DataStreams {

		logrus.Debugf("Process data stream index %s", datastreamIndex.Name)

		for i, indice := range datastreamIndex.Indices {

			logrus.Debugf("Check index %s", indice.IndexName)

			index, err := os.Indices().Get(ctx, []string{indice.IndexName})
			if err != nil {
				return errors.Wrapf(err, "error to get index %s", indice.IndexName)
			}

			if index[indice.IndexName] != nil {
				unixTimeStampStr := index[indice.IndexName].Settings["index"].(map[string]any)["creation_date"].(string)
				logrus.Debugf("Index creation time: %s", unixTimeStampStr)

				unixTimeStamp, err := strconv.ParseInt(unixTimeStampStr, 10, 64)
				if err != nil {
					return errors.Wrapf(err, "error to parse index creation time %s", unixTimeStampStr)
				}
				// Convert unix to date time
				creationDate = time.Unix(unixTimeStamp/1000, 0)
			} else {
				return errors.Errorf("error to get index %s", indice.IndexName)
			}

			// Check if is the starting index
			if !isFoundStartingIndex && creationDate.After(fromDateTime) {
				if i > 0 {
					logrus.Debugf("Found starting index %s", datastreamIndex.Indices[i-1].IndexName)
					// Process previous index
					logrus.Infof("Process index %s", datastreamIndex.Indices[i-1].IndexName)
					if err = handleClosedIndex(ctx, metadata, querySize, fromDate, toDate, dateField, datastreamIndex.Indices[i-1].IndexName, query, fields, separator, splitFileColumn, path, pitDuration, os); err != nil {
						return err
					}

				} else {
					logrus.Debugf("Found starting index %s", indice.IndexName)
				}
				isFoundStartingIndex = true
			}

			if isFoundStartingIndex && creationDate.Before(toDateTime) {
				// Process index
				logrus.Infof("Process index %s", indice.IndexName)
				if err = handleClosedIndex(ctx, metadata, querySize, fromDate, toDate, dateField, indice.IndexName, query, fields, separator, splitFileColumn, path, pitDuration, os); err != nil {
					return err
				}
			}

			// Check if is the ending index
			if isFoundStartingIndex && creationDate.After(toDateTime) {
				logrus.Debugf("Found ending index %s", indice.IndexName)
				logrus.Infof("Process index %s", indice.IndexName)
				if err = handleClosedIndex(ctx, metadata, querySize, fromDate, toDate, dateField, indice.IndexName, query, fields, separator, splitFileColumn, path, pitDuration, os); err != nil {
					return err
				}
				break
			}
		}
	}

	return nil

}

// handleClosedIndex permit to open index if it's closed, process it and close it if it's not used by another session
func handleClosedIndex(ctx context.Context, metadata *Metadata, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, pitDuration string, os opensearch.Client) (err error) {
	indexState, err := os.Cat().Indices(ctx, []string{index})
	if err != nil {
		return errors.Wrapf(err, "error to get index %s", index)
	}

	if indexState[0].Status == "close" {

		// Reopen index
		if resp, err := os.Indices().Open(ctx, index); err != nil || !resp.Acknowledged {
			return errors.Wrapf(err, "error to open index %s", index)
		}
		defer func() {

			// Check if index is already used on other session
			// If not, close it
			canBeClosed, err := isIndexCanBeClosed(ctx, index, os)
			if err != nil {
				log.Errorf("error to check if index %s can be closed", index)
				return
			}

			if !canBeClosed {
				logrus.Infof("Index %s elaready used by another session, skip close it", index)
				return
			}

			// Close index
			if resp, err := os.Indices().Close(ctx, index); err != nil || !resp.Acknowledged {
				log.Errorf("error to close index %s", index)
				return
			}
			logrus.Infof("Close index %s", index)

			// Remove index from metadata
			for i, idx := range metadata.Indexes {
				if idx == index {
					metadata.Indexes = append(metadata.Indexes[:i], metadata.Indexes[i+1:]...)
					break
				}

				if _, err = os.Document().Index(ctx, &api.IndexRequest{Index: metadataIndexName, Id: metadata.Id, Body: metadata}); err != nil {
					log.Errorf("error to update metadata %s", metadata.Id)
				}
			}
		}()
		logrus.Infof("Open index %s", index)

		metadata.Indexes = append(metadata.Indexes, index)
		if _, err = os.Document().Index(ctx, &api.IndexRequest{Index: metadataIndexName, Id: metadata.Id, Body: metadata}); err != nil {
			return errors.Wrapf(err, "error to update metadata %s", metadata.Id)
		}

	}

	return exportDataToFilesWithoutClosedIndex(ctx, querySize, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, pitDuration, os)
}

func processExport(searchResult *querydsl.SearchResult, fields []string, separator string, path string, splitFileColumn string) (err error) {
	log.Debugf("Process %d documents", len(searchResult.Hits.Hits))

	// Loop over results
	if len(searchResult.Hits.Hits) > 0 {
		listFiles := make(map[string]*os.File, 0)

		for _, item := range searchResult.Hits.Hits {

			// Create target file to write result
			jsonResult := gjson.ParseBytes(item.Source)

			fileName := fmt.Sprintf("%s/%s", path, jsonResult.Get(splitFileColumn))
			file, ok := listFiles[fileName]
			if !ok {
				if _, err = os.Stat(fileName); os.IsNotExist(err) {
					log.Infof("Create file: %s", fileName)
				}
				log.Debugf("Open file %s", fileName)
				file, err = os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				if err != nil {
					log.Errorf("Error when open file: %s", err.Error())
					return err
				}
				defer func() { _ = file.Close() }()

				listFiles[fileName] = file
			}
			// Extract needed columns
			td := make([]string, 0)
			for _, field := range fields {
				td = append(td, jsonResult.Get(field).Str)
			}

			// Write result
			_, err := fmt.Fprintf(file, "%s\n", strings.Join(td, separator))
			if err != nil {
				log.Errorf("Error when write file: %s", err.Error())
				return err
			}
		}

	}

	return nil
}
