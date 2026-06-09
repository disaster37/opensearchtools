package opensearchtools

import (
	"context"
	"os"
	"time"

	"github.com/disaster37/opensearch/v4"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func manageOpensearchGlobalParameters(c *cli.Context) (opensearch.Client, error) {
	log.Debug("Opensearch URL: ", c.String("url"))
	log.Debug("Opensearch user: ", c.String("user"))
	log.Debug("Opensearch password: XXX")
	log.Debug("Disable verify SSL: ", c.Bool("self-signed-certificate"))

	// Init opensearch client
	cfg := &opensearch.Config{
		URL:              c.String("url"),
		Username:         c.String("user"),
		Password:         c.String("password"),
		RetryCount:       10,
		RetryWaitTime:    1 * time.Second,
		RetryMaxWaitTime: 10 * time.Second,
	}
	if c.Bool("self-signed-certificate") {
		cfg.TLSSkipVerify = true
	}

	os, err := opensearch.New(cfg, log.NewEntry(log.StandardLogger()))
	if err != nil {
		return nil, err
	}

	return os, nil
}

// CheckConnexion check if the connexion to opensearch is OK
func CheckConnexion(c *cli.Context) error {
	osClient, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	return checkConnexion(c.Context, osClient)
}

func checkConnexion(ctx context.Context, os opensearch.Client) error {
	_, err := os.Cluster().Health(ctx, nil)
	if err != nil {
		return errors.Errorf("Error when check Opensearch connexion: %s", err.Error())
	}

	return nil
}

// CheckClusterStatus check the status of the cluster
func CheckClusterStatus(c *cli.Context) error {
	osClient, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		log.Errorf("Cluster Unknown:\n%s", err.Error())
		os.Exit(3)
	}

	status, err := checkClusterStatus(c.Context, osClient)
	if err != nil {
		log.Errorf("Cluster Unknown:\n%s", err.Error())
		os.Exit(3)
	}

	switch status {
	case "green":
		log.Info("Cluster OK")
		return nil
	case "yellow":
		log.Info("Cluster warning")
		os.Exit(1)
	case "red":
		log.Info("Cluster critical")
		os.Exit(2)
	}

	return nil
}

func checkClusterStatus(ctx context.Context, os opensearch.Client) (string, error) {
	res, err := os.Cluster().Health(ctx, nil)
	if err != nil {
		return "", err
	}

	return res.Status, nil
}

// ClusterEnableRoutingAllocation enable routing allocation
func ClusterEnableRoutingAllocation(c *cli.Context) error {
	os, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	err = enableRoutingAllocation(c.Context, os)
	if err != nil {
		return err
	}

	log.Info("Enable routing allocation successfully")

	return nil
}

// ClusterDisableRoutingAllocation disable routing allocation
func ClusterDisableRoutingAllocation(c *cli.Context) error {
	os, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	err = disableRoutingAllocation(c.Context, os)
	if err != nil {
		return err
	}

	log.Info("Disable routing allocation successfully")

	return nil
}

func enableRoutingAllocation(ctx context.Context, os opensearch.Client) error {
	settings := map[string]interface{}{
		"persistent": map[string]interface{}{
			"cluster.routing.allocation.enable": "all",
		},
	}

	err := putClusterSettings(ctx, os, settings)

	return err
}

func disableRoutingAllocation(ctx context.Context, os opensearch.Client) error {
	settings := map[string]interface{}{
		"persistent": map[string]interface{}{
			"cluster.routing.allocation.enable": "primaries",
		},
	}

	err := putClusterSettings(ctx, os, settings)

	return err
}

func putClusterSettings(ctx context.Context, os opensearch.Client, settings map[string]interface{}) error {
	log.Debugf("Settings: %+v", settings)

	if _, err := os.Cluster().PutSettings(ctx, settings); err != nil {
		return errors.Wrapf(err, "Error when set Opensearch cluster setting")
	}

	return nil
}
