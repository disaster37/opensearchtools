package opensearchtools

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"

	"github.com/disaster37/opensearch/v2"
	"github.com/disaster37/opensearch/v2/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/utils/ptr"
)

func manageOpensearchGlobalParameters(c *cli.Context) (*opensearch.Client, error) {

	log.Debug("Opensearch URL: ", c.String("urls"))
	log.Debug("Opensearch user: ", c.String("user"))
	log.Debug("Opensearch password: XXX")
	log.Debug("Disable verify SSL: ", c.Bool("self-signed-certificate"))

	// Init opensearch client
	cfg := &config.Config{
		URLs:        c.StringSlice("urls"),
		Username:    c.String("user"),
		Password:    c.String("password"),
		Sniff:       ptr.To[bool](false),
		Healthcheck: ptr.To[bool](false),
	}
	if c.Bool("self-signed-certificate") {
		cfg.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	es, err := opensearch.NewClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	return es, nil

}

func CheckConnexion(c *cli.Context) error {

	es, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	return checkConnexion(es)
}

func checkConnexion(es *opensearch.Client) error {

	_, err := es.ClusterHealth().Do(context.Background())
	if err != nil {
		return errors.Errorf("Error when check Opensearch connexion: %s", err.Error())
	}

	return nil
}

func CheckClusterStatus(c *cli.Context) error {

	es, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		log.Errorf("Cluster Unknown:\n%s", err.Error())
		os.Exit(3)
	}

	status, err := checkClusterStatus(es)
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

func checkClusterStatus(es *opensearch.Client) (string, error) {
	res, err := es.ClusterHealth().Do(context.Background())
	if err != nil {
		return "", err
	}

	return res.Status, nil

}

func ClusterEnableRoutingAllocation(c *cli.Context) error {

	es, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	err = enableRoutingAllocation(es)
	if err != nil {
		return err
	}

	log.Info("Enable routing allocation successfully")

	return nil
}

func ClusterDisableRoutingAllocation(c *cli.Context) error {

	es, err := manageOpensearchGlobalParameters(c)
	if err != nil {
		return err
	}

	err = disableRoutingAllocation(es)
	if err != nil {
		return err
	}

	log.Info("Disable routing allocation successfully")

	return nil
}

func enableRoutingAllocation(es *opensearch.Client) error {
	settings := map[string]interface{}{
		"persistent": map[string]interface{}{
			"cluster.routing.allocation.enable": "all",
		},
	}

	err := putClusterSettings(es, settings)

	return err
}

func disableRoutingAllocation(es *opensearch.Client) error {
	settings := map[string]interface{}{
		"persistent": map[string]interface{}{
			"cluster.routing.allocation.enable": "primaries",
		},
	}

	err := putClusterSettings(es, settings)

	return err
}

func putClusterSettings(es *opensearch.Client, settings map[string]interface{}) error {

	log.Debugf("Settings: %+v", settings)

	if _, err := es.ClusterPutSetting().Body(settings).Do(context.Background()); err != nil {
		return errors.Wrapf(err, "Error when set Opensearch cluster setting")
	}

	return nil

}
