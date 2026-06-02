package opensearchtools

import (
	"context"
	"os"

	"github.com/disaster37/opensearch/v4"
	"github.com/disaster37/opensearch/v4/api"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// CheckNodeOnline check if node is online
func CheckNodeOnline(c *cli.Context) error {
	osClient, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	isOnline, err := checkNodeOnline(c.Context, osClient, c.String("node-name"), c.StringSlice("labels"))
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

// CheckExpectedNumberNodes check if the number of nodes is the expected
func CheckExpectedNumberNodes(c *cli.Context) error {
	osClient, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	isExpected, err := checkExpectedNumberNodes(c.Context, osClient, c.Int("number-nodes"))
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

func checkNodeOnline(ctx context.Context, os opensearch.Client, nodeName string, labels []string) (bool, error) {
	nodesInfo, err := os.Nodes().Info(ctx, &api.NodesInfoRequest{})
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

func checkExpectedNumberNodes(ctx context.Context, os opensearch.Client, nodesNumber int) (bool, error) {
	nodesInfo, err := os.Nodes().Info(ctx, &api.NodesInfoRequest{})
	if err != nil {
		return false, errors.Wrapf(err, "Error when get nodes info")
	}

	log.Debugf("Found %d nodes in cluster", len(nodesInfo.Nodes))

	return len(nodesInfo.Nodes) == nodesNumber, nil
}
