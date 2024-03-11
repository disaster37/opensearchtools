package opensearchtools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/disaster37/opensearch/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
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

	if path == "" {
		return errors.New("You must set --path")
	}
	if splitFileField == "" {
		return errors.New("You must set --split-file-field")
	}
	if query == "" {
		return errors.New("You must set --query")
	}

	err = exportDataToFiles(from, to, dateField, index, query, fields, separator, splitFileField, path, es)
	if err != nil {
		return err
	}

	log.Infof("Extract successfully")

	return nil
}

func exportDataToFiles(fromDate string, toDate string, dateField string, index string, query string, fields []string, separator string, splitFileColumn string, path string, es *opensearch.Client) error {

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
		//DocvalueFields(computedFields...).
		Size(size).
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
				file, err = os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					log.Errorf("Error when open file: %s", err.Error())
					return err
				}
				defer file.Close()

				listFiles[fileName] = file
			}
			// Extract needed columns
			td := make([]string, 0)
			for _, field := range fields {
				td = append(td, jsonResult.Get(field).Str)
			}

			// Write result
			_, err := file.WriteString(fmt.Sprintf("%s\n", strings.Join(td, separator)))
			if err != nil {
				log.Errorf("Error when write file: %s", err.Error())
				return err
			}
		}

	}

	return nil
}
