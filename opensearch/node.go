package opensearchtools

import (
	"context"
	"os"

	"github.com/disaster37/opensearch/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func CheckNodeOnline(c *cli.Context) error {

	es, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	isOnline, err := checkNodeOnline(es, c.String("node-name"), c.StringSlice("labels"))
	if err != nil {
		return err
	}

	if isOnline {
		log.Infof("Node %s is on cluster", c.String("node-name"))
		return nil
	}

	log.Warnf("Node %s not yet on cluster", c.String("node-name"))
	os.Exit(1)

	return nil

}

func CheckExpectedNumberNodes(c *cli.Context) error {

	es, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	isExpected, err := checkExpectedNumberNodes(es, c.Int("number-nodes"))
	if err != nil {
		return err
	}

	if isExpected {
		log.Infof("All nodes in cluster (%d)", c.Int("number-nodes"))
		return nil
	}

	log.Warnf("The are some nodes lost. We expect %d nodes", c.Int("number-nodes"))
	os.Exit(1)

	return nil

}

func checkNodeOnline(es *opensearch.Client, nodeName string, labels []string) (bool, error) {
	nodesInfo, err := es.NodesInfo().Do(context.Background())
	if err != nil {
		return false, errors.Wrapf(err, "Error when get nodes info")
	}

	for _, node := range nodesInfo.Nodes {
		if node.Name == nodeName {
			return true, nil
		}

		for _, label := range labels {
			if node.Attributes[label] == nodeName {
				return true, nil
			}
		}
	}

	return false, nil

}

func checkExpectedNumberNodes(es *opensearch.Client, nodesNumber int) (bool, error) {
	nodesInfo, err := es.NodesInfo().Do(context.Background())
	if err != nil {
		return false, errors.Wrapf(err, "Error when get nodes info")
	}

	log.Debugf("Found %d nodes in cluster", len(nodesInfo.Nodes))

	return len(nodesInfo.Nodes) == nodesNumber, nil
}
