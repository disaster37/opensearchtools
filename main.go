package main

import (
	"fmt"
	"os"
	"sort"

	localopensearch "github.com/disaster37/opensearchtools/v2/opensearch"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

var (
	version string
	commit  string
)

func run(args []string) error {

	// Logger setting
	formatter := new(prefixed.TextFormatter)
	formatter.FullTimestamp = true
	formatter.ForceFormatting = true
	log.SetFormatter(formatter)
	log.SetOutput(os.Stdout)

	// CLI settings
	app := cli.NewApp()
	app.Usage = "Manage Opensearch on cli interface"
	app.Version = fmt.Sprintf("%s-%s", version, commit)
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "Load configuration from `FILE`",
		},
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "urls",
			Usage:   "The opensearch URLs",
			EnvVars: []string{"OPENSEARCH_URLS"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "user",
			Usage:   "The  user",
			EnvVars: []string{"OPENSEARCH_USER"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "password",
			Usage:   "The password",
			EnvVars: []string{"OPENSEARCH_PASSWORD"},
		}),
		&cli.BoolFlag{
			Name:  "self-signed-certificate",
			Usage: "Disable the TLS certificate check",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Display debug output",
		},
	}
	app.Commands = []*cli.Command{
		{
			Name:     "check-connexion-opensearch",
			Usage:    "Check the opensearch connexion",
			Category: "Check",
			Action:   localopensearch.CheckConnexion,
		},
		{
			Name:     "check-opensearch-status",
			Usage:    "Check the opensearch status",
			Category: "Check",
			Action:   localopensearch.CheckClusterStatus,
		},
		{
			Name:     "check-node-online",
			Usage:    "Check the node is online on Opensearch cluster",
			Category: "Check",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "node-name",
					Usage:    "The node name",
					Required: true,
				},
				&cli.StringSliceFlag{
					Name:     "labels",
					Usage:    "The labels to check the node name",
					Required: false,
				},
			},
			Action: localopensearch.CheckNodeOnline,
		},
		{
			Name:     "check-number-nodes",
			Usage:    "Check there are a number of node in cluster",
			Category: "Check",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:     "number-nodes",
					Usage:    "The number of node expected",
					Required: true,
				},
			},
			Action: localopensearch.CheckExpectedNumberNodes,
		},
		{
			Name:     "disable-routing-allocation",
			Usage:    "Disable routing allocation on Opensearch cluster",
			Category: "Downtime",
			Action:   localopensearch.ClusterDisableRoutingAllocation,
		},
		{
			Name:     "enable-routing-allocation",
			Usage:    "Enable routing allocation on Opensearch cluster",
			Category: "Downtime",
			Action:   localopensearch.ClusterEnableRoutingAllocation,
		},
	}

	app.Before = func(c *cli.Context) error {

		if c.Bool("debug") {
			log.SetLevel(log.DebugLevel)
		}

		if c.String("config") != "" {
			before := altsrc.InitInputSourceWithContext(app.Flags, altsrc.NewYamlSourceFromFlagFunc("config"))
			return before(c)
		}
		return nil
	}

	sort.Sort(cli.CommandsByName(app.Commands))

	err := app.Run(args)
	return err
}

func main() {
	err := run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
