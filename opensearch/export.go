package opensearchtools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/disaster37/opensearch/v3"
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
	es, err := manageOpensearchGlobalParameters(c)
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

	if path == "" {
		return errors.New("You must set --path")
	}
	if splitFileField == "" {
		return errors.New("You must set --split-file-field")
	}
	if query == "" {
		return errors.New("You must set --query")
	}

	err = exportDataToFiles(from, to, dateField, index, query, isOpenClosedIndex, fields, separator, splitFileField, path, es)
	if err != nil {
		return err
	}

	log.Infof("Extract successfully")

	return nil
}

func exportDataToFiles(fromDate string, toDate string, dateField string, index string, query string, isOpenClosedIndex bool, fields []string, separator string, splitFileColumn string, path string, es *opensearch.Client) error {
	if path == "" {
		return errors.New("You must provide path")
	}
	if index == "" {
		return errors.New("You must provide index")
	}

	if dateField == "" {
		return errors.New("You must provide date-field")
	}

	if es == nil {
		return errors.New("You must provide es client")
	}

	ctx := context.Background()
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

	// Search on all index, without needed to work index by index
	if !isOpenClosedIndex {
		return exportDataToFilesWithoutClosedIndex(ctx, size, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, es)
	} else {
		// Work index by index
		return exportDataToFilesWithClosedIndex(ctx, size, fromDate, toDate, dateField, index, query, fields, separator, splitFileColumn, path, es)
	}
}

func exportDataToFilesWithoutClosedIndex(ctx context.Context, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, es *opensearch.Client) error {

	// Build query
	rangeDateQuery := opensearch.NewRangeQuery(dateField).
		Gte(fromDate).
		Lte(toDate)
	stringQuery := opensearch.NewQueryStringQuery(query).
		AnalyzeWildcard(true)
	boolQuery := opensearch.NewBoolQuery().Must(rangeDateQuery, stringQuery)

	// Forge payload
	computedFields := append(fields, splitFileColumn)
	scs := es.Scroll(index).
		// DocvalueFields(computedFields...).
		Size(querySize).
		Query(boolQuery).
		Sort(dateField, true).
		FetchSourceContext(opensearch.NewFetchSourceContext(true).Include(computedFields...)).
		TrackTotalHits(true)

	// Get records over scroll
	firstLoop := true
	for {
		searchResult, err := scs.Do(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if firstLoop {
			firstLoop = false
			log.Infof("Found %d document to export", searchResult.TotalHits())
		}

		if err = processExport(searchResult, fields, separator, path, splitFileColumn); err != nil {
			return err
		}
	}

	return nil

}

func exportDataToFilesWithClosedIndex(ctx context.Context, querySize int, fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, es *opensearch.Client) error {

	// Check if data stream index first
	datastreamIndexResponse, err := es.GetDataStreamIndex(index).Do(ctx)
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

	// Build query
	rangeDateQuery := opensearch.NewRangeQuery(dateField).
		Gte(fromDate).
		Lte(toDate)
	stringQuery := opensearch.NewQueryStringQuery(query).
		AnalyzeWildcard(true)
	boolQuery := opensearch.NewBoolQuery().Must(rangeDateQuery, stringQuery)

	var creationDate time.Time
	isFoundStartingIndex := false

	// Loop over index and search the index creation time that match the date range
	for _, datastreamIndex := range datastreamIndexResponse.Datastreams {

		logrus.Debugf("Process data stream index %s", datastreamIndex.Name)

		for i, indice := range datastreamIndex.Indices {

			logrus.Debugf("Check index %s", indice.IndexName)

			index, err := es.IndexGet(indice.IndexName).Do(ctx)
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
					if err = processIndex(ctx, datastreamIndex.Indices[i-1].IndexName, querySize, boolQuery, fields, dateField, separator, splitFileColumn, path, es); err != nil {
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
				if err = processIndex(ctx, indice.IndexName, querySize, boolQuery, fields, dateField, separator, splitFileColumn, path, es); err != nil {
					return err
				}
			}

			// Check if is the ending index
			if isFoundStartingIndex && creationDate.After(toDateTime) {
				logrus.Debugf("Found ending index %s", indice.IndexName)
				logrus.Infof("Process index %s", indice.IndexName)
				if err = processIndex(ctx, indice.IndexName, querySize, boolQuery, fields, dateField, separator, splitFileColumn, path, es); err != nil {
					return err
				}
				break
			}
		}
	}

	return nil

}

func processIndex(ctx context.Context, index string, querySize int, query opensearch.Query, fields []string, dateField, separator, splitFileColumn, path string, es *opensearch.Client) (err error) {
	indexState, err := es.CatIndices().Index(index).Do(ctx)
	if err != nil {
		return errors.Wrapf(err, "error to get index %s", index)
	}

	if indexState[0].Status == "close" {
		// Reopen index
		_, err = es.OpenIndex(index).Do(ctx)
		if err != nil {
			return errors.Wrapf(err, "error to open index %s", index)
		}
		defer func() {
			_, err = es.CloseIndex(index).Do(ctx)
			if err != nil {
				log.Errorf("error to close index %s", index)
			}
		}()
	}

	// Forge payload
	computedFields := append(fields, splitFileColumn)
	scs := es.Scroll(index).
		// DocvalueFields(computedFields...).
		Size(querySize).
		Query(query).
		Sort(dateField, true).
		FetchSourceContext(opensearch.NewFetchSourceContext(true).Include(computedFields...)).
		TrackTotalHits(true)

	// Get records over scroll
	firstLoop := true
	for {
		searchResult, err := scs.Do(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if firstLoop {
			firstLoop = false
			log.Infof("Found %d document to export", searchResult.TotalHits())
		}

		if err = processExport(searchResult, fields, separator, path, splitFileColumn); err != nil {
			return err
		}
	}

	return nil
}

func processExport(searchResult *opensearch.SearchResult, fields []string, separator string, path string, splitFileColumn string) (err error) {
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
