package opensearchtools

import (
	"context"
	"fmt"
	stdos "os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/disaster37/opensearch/v4"
	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/timberio/go-datemath"
	"github.com/urfave/cli/v2"
)

// errStopIteration is a sentinel error used to signal forEachIndexInDateRange
// to stop iteration early when the user declines to continue.
var errStopIteration = errors.New("stop iteration")

// promptConfirmFunc is the function used to prompt the user for confirmation.
// It can be overridden in tests to simulate user input without an interactive terminal.
var promptConfirmFunc = func(label string) bool {
	p := promptui.Prompt{
		Label:     label,
		IsConfirm: true,
	}

	var response string

	for response != "y" && response != "n" && response != "N" {
		response, _ = p.Run()
	}

	return response == "y"
}

// OpenClosedIndex permit to open index that are closed
// It track them on metadata index
func OpenClosedIndex(c *cli.Context) error {
	os, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	return openClosedIndex(c.Context, c.String("from"), c.String("to"), c.String("index"), c.Int("max-number-indexes"), os)
}

func openClosedIndex(ctx context.Context, from string, to string, index string, maxNumberIndexes int, osClient opensearch.Client) (err error) {
	logrus.Debugf("From: %s", from)
	logrus.Debugf("To: %s", to)
	logrus.Debugf("Index: %s", index)
	logrus.Debugf("Max number indexes: %d", maxNumberIndexes)

	fromDateExpr, err := datemath.Parse(from)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", from)
	}
	fromDateTime := fromDateExpr.Time()

	toDateExpr, err := datemath.Parse(to)
	if err != nil {
		return errors.Wrapf(err, "error to parse date %s", to)
	}
	toDateTime := toDateExpr.Time()

	// Create metadata index
	if err = createMetadataindexIfNotExist(ctx, osClient); err != nil {
		return err
	}

	// Get current user
	authResponse, err := osClient.Security().AuthInfo(ctx)
	if err != nil {
		return errors.Wrap(err, "error to get current user")
	}

	// Create metadata document
	metadata := &Metadata{
		Type:      MetadataTypeExploreAutoOpenIndex,
		User:      authResponse.UserName,
		SessionId: uuid.New().String(),
		Indexes:   []string{},
	}
	if err = createMetdata(ctx, metadata, osClient); err != nil {
		return errors.Wrap(err, "error to create metadata document")
	}

	defer func() {
		// Delete metadata document
		if err := cleanMetadataExploreWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, false, osClient); err != nil {
			logrus.Errorf("error to delete metadata document %s: %v", metadata.Id, err)
		}
	}()

	// Register signal handler for graceful cleanup on Ctrl+C / kill
	sigCh := make(chan stdos.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logrus.Warn("Received termination signal, cleaning up...")
		if cleanErr := cleanMetadataExploreWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, false, osClient); cleanErr != nil {
			logrus.Errorf("Error during cleanup: %v", cleanErr)
		}
		stdos.Exit(1)
	}()
	defer signal.Stop(sigCh)

	logrus.Infof("Loop over index in datastream %s to found the starting index", index)

	currentOpenIndex := maxNumberIndexes

	fn := func(ctx context.Context, indexName string) error {
		if currentOpenIndex == 0 {
			if !promptConfirmFunc(fmt.Sprintf("Continue with the next %d indexes", maxNumberIndexes)) {
				return errStopIteration
			}

			if err := cleanMetadataExploreWithAutoOpenIndex(context.Background(), authResponse.UserName, metadata.SessionId, true, osClient); err != nil {
				return errors.Wrap(err, "error to clean metadata")
			}

			// Reset local metadata Indexes to avoid polluting the OpenSearch
			// document with stale entries from previous batches.
			metadata.Indexes = []string{}

			currentOpenIndex = maxNumberIndexes
		}

		isOpenIndex, err := lockIndex(ctx, indexName, metadata, osClient)
		if err != nil {
			return err
		}

		if isOpenIndex {
			currentOpenIndex--
		}
		return nil
	}

	if err := forEachIndexInDateRange(ctx, osClient, index, fromDateTime, toDateTime, fn); err != nil {
		return err
	}

	// ask user to continue by press continue
	for !promptConfirmFunc("Close all indexes") {
		continue
	}

	return nil
}

// forEachIndexInDateRange iterates over datastream indices within a date range
// and calls fn for each matched index.
func forEachIndexInDateRange(ctx context.Context, os opensearch.Client, datastreamIndex string, fromDateTime, toDateTime time.Time, fn func(ctx context.Context, indexName string) error) error {
	datastreamIndexResponse, err := os.Indices().GetDataStream(ctx, []string{datastreamIndex})
	if err != nil {
		return errors.Errorf("The index %s is not a data stream index. It must be a data stream index", datastreamIndex)
	}

	var creationDate time.Time

	for _, datastream := range datastreamIndexResponse.DataStreams {
		logrus.Debugf("Process data stream index %s", datastream.Name)
		isFoundStartingIndex := false

		callFn := func(ctx context.Context, indexName string) error {
			logrus.Infof("Process index %s", indexName)
			if err := fn(ctx, indexName); err != nil {
				if errors.Is(err, errStopIteration) {
					return nil
				}
				return err
			}
			return nil
		}

		for i, indice := range datastream.Indices {
			logrus.Debugf("Check index %s", indice.IndexName)

			indexInfo, err := os.Indices().Get(ctx, []string{indice.IndexName})
			if err != nil {
				return errors.Wrapf(err, "error to get index %s", indice.IndexName)
			}

			if indexInfo[indice.IndexName] != nil {
				unixTimeStampStr := indexInfo[indice.IndexName].Settings["index"].(map[string]any)["creation_date"].(string)
				logrus.Debugf("Index creation time: %s", unixTimeStampStr)

				unixTimeStamp, err := strconv.ParseInt(unixTimeStampStr, 10, 64)
				if err != nil {
					return errors.Wrapf(err, "error to parse index creation time %s", unixTimeStampStr)
				}
				creationDate = time.Unix(unixTimeStamp/1000, 0)
			} else {
				return errors.Errorf("error to get index %s", indice.IndexName)
			}

			if !isFoundStartingIndex && creationDate.After(fromDateTime) {
				if i > 0 {
					logrus.Debugf("Found starting index %s", datastream.Indices[i-1].IndexName)
					if err := callFn(ctx, datastream.Indices[i-1].IndexName); err != nil {
						return err
					}
				} else {
					logrus.Debugf("Found starting index %s", indice.IndexName)
				}
				isFoundStartingIndex = true
			}

			if isFoundStartingIndex && creationDate.Before(toDateTime) {
				if err := callFn(ctx, indice.IndexName); err != nil {
					return err
				}
			}

			if isFoundStartingIndex && creationDate.After(toDateTime) {
				logrus.Debugf("Found ending index %s", indice.IndexName)
				if err := callFn(ctx, indice.IndexName); err != nil {
					return err
				}
				break
			}
		}
	}

	return nil
}
