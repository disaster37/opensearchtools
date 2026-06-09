// A generated module for Opensearchtools functions
//
// This module has been generated via dagger init and serves as a reference to
// basic module structure as you get started with Dagger.
//
// Two functions have been pre-created. You can modify, delete, or add to them,
// as needed. They demonstrate usage of arguments and return types using simple
// echo and grep commands. The functions can be called from the dagger CLI or
// from one of the SDKs.
//
// The first line in this comment block is a short description line and the
// rest is a long description with more detail on the module's purpose or usage,
// if appropriate. All modules should have a short description.

package main

import (
	"context"
	"fmt"

	"dagger/opensearchtools/internal/dagger"

	"emperror.dev/errors"
	"github.com/disaster37/dagger-library-go/lib/helper"
)

const (
	OpensearchVersion string = "3.4.0"
	username          string = "admin"
	password          string = "vLPeJYa8.3RqtZCcAK6jNz"
	mockgenVersion           = "v0.3.0"
	gitUsername       string = "ci"
	gitEmail          string = "ci@localhost"
	defaultGitBranch  string = "3.x"
	registry          string = "quay.io"
	repository        string = "webcenter/opensearchtools"
)

type Opensearchtools struct {
	// Src is a directory that contains the projects source code
	// +private
	Src *dagger.Directory

	// +private
	GolangModule *dagger.Golang
}

func New(
	ctx context.Context,
	// a path to a directory containing the source code
	// +required
	src *dagger.Directory,
) (*Opensearchtools, error) {
	return &Opensearchtools{
		Src:          src,
		GolangModule: dag.Golang(src),
	}, nil
}

func (h *Opensearchtools) Ci(
	ctx context.Context,

	// Set tru if you are on CI
	// +default=false
	ci bool,

	// The image version to publish
	// +optional
	version string,

	// The registry username
	// +optional
	registryUsername *dagger.Secret,

	// The registry password
	// +optional
	registryPassword *dagger.Secret,

	// The codeCov token
	// +optional
	codeCoveToken *dagger.Secret,

	// The git branch where you should to push
	// You need to provide it when you are on PullRequest or on Tag
	// +optional
	gitBranch string,

	// Set true if current build is a tag
	// It will use the stable and alpha channel
	// alpha channel only instead
	// +optional
	isTag bool,

	// The git token
	// +optional
	gitToken *dagger.Secret,
) (dir *dagger.Directory, err error) {
	var stdout string

	// Build
	h.Build(ctx)

	// Lint code
	stdout, err = h.Lint(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "Error when lint project: %s", stdout)
	}

	// Format code
	dir = h.Format(ctx)

	// Test code
	reportFile, err := h.Test(ctx, false, false, "", "", "")
	if err != nil {
		return nil, errors.Wrapf(err, "Error when test project: %s", stdout)
	}

	// Generate coverage report
	dir = dir.WithFile("coverage.out", reportFile)

	if ci {
		if codeCoveToken == nil {
			return nil, errors.New("You need to provide CodeCov token")
		}
		stdout, err = h.CodeCov(ctx, dir, codeCoveToken)
		if err != nil {
			return nil, errors.Wrapf(err, "Error when upload report on CodeCov: %s", stdout)
		}

		git := dag.GitModule(dir.WithDirectory("ci", h.Src.Directory("ci")), dagger.GitModuleOpts{Ci: "github"}).
			SetConfig(dagger.GitModuleSetConfigOpts{
				Username: gitUsername,
				Email:    gitEmail,
			})

		if isTag {
			gitBranch = defaultGitBranch
		}

		if _, err = git.CommitAndPush(
			ctx,
			gitToken,
			dagger.GitModuleCommitAndPushOpts{
				BranchName: gitBranch,
				GitRepoURL: "https://github.com/disaster37/opensearch.git",
				Message:    "Commit from CI",
			},
		); err != nil {
			return nil, errors.Wrap(err, "Error when commit and push files change")
		}
	}

	// Build docker image
	if err = h.BuildImage(ctx, ci, version, registryUsername, registryPassword); err != nil {
		return nil, errors.Wrap(err, "Error when build and push Docker image")
	}

	return dir, nil
}

// Lint permit to lint code
func (h *Opensearchtools) Lint(
	ctx context.Context,
) (string, error) {
	return h.GolangModule.Lint(ctx)
}

// Format permit to format the golang code
func (h *Opensearchtools) Format(
	ctx context.Context,
) *dagger.Directory {
	return h.GolangModule.Format()
}

func (h *Opensearchtools) Opensearch(
	ctx context.Context,
) (*dagger.Service, error) {
	os := dag.Container().
		From(fmt.Sprintf("opensearchproject/opensearch:%s", OpensearchVersion)).
		WithEnvVariable("cluster.name", "test").
		WithEnvVariable("node.name", "opensearch-node1").
		WithEnvVariable("bootstrap.memory_lock", "true").
		WithEnvVariable("discovery.type", "single-node").
		WithEnvVariable("network.publish_host", "127.0.0.1").
		WithEnvVariable("logger.org.opensearchsearch", "warn").
		WithEnvVariable("OPENSEARCH_JAVA_OPTS", "-Xms1g -Xmx1g").
		WithEnvVariable("plugins.security.nodes_dn_dynamic_config_enabled", "true").
		WithEnvVariable("plugins.security.unsupported.restapi.allow_securityconfig_modification", "true").
		WithEnvVariable("OPENSEARCH_INITIAL_ADMIN_PASSWORD", password).
		WithExposedPort(9200).
		AsService()

	_, err := h.GolangModule.Container().
		WithServiceBinding("opensearch.svc", os).
		WithEnvVariable("OPENSEARCH_USERNAME", username).
		WithEnvVariable("OPENSEARCH_PASSWORD", password).
		WithExec(helper.ForgeScript(`
set -e
sleep 10
curl --fail -XGET -k -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD "https://opensearch.svc:9200/_cluster/health?wait_for_status=yellow&timeout=500s"
curl --fail -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD -k -H "Content-Type: application/x-ndjson" -XPOST https://opensearch.svc:9200/logs/_bulk?refresh=wait_for --data-binary @fixtures/logs/bulk.ndjson
curl --fail -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD -k -H "Content-Type: application/json" -XPUT https://opensearch.svc:9200/_index_template/ds -d @fixtures/logs/index_template.json
curl --fail -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD -k -H "Content-Type: application/json" -XPUT https://opensearch.svc:9200/_data_stream/test
curl --fail -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD -k -H "Content-Type: application/json" -XPUT https://opensearch.svc:9200/_data_stream/test-metadata
curl --fail -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD -k -H "Content-Type: application/x-ndjson" -XPOST https://opensearch.svc:9200/test/_bulk?refresh=wait_for --data-binary @fixtures/logs/bulk.ndjson
sleep 10
`)).Sync(ctx)
	if err != nil {
		return nil, err
	}

	return os, nil
}

// Test permit to run tests
func (h *Opensearchtools) Test(
	ctx context.Context,
	//+optional
	short bool,
	//+optional
	shuffle bool,
	//+optional
	run string,
	//+optional
	skip string,
	//+optional
	path string,
) (*dagger.File, error) {
	// Run Opensearch
	opensearchService, err := h.Opensearch(ctx)
	if err != nil {
		return nil, err
	}

	goContainer := h.GolangModule.Container().
		WithServiceBinding("opensearch.svc", opensearchService).
		WithEnvVariable("OPENSEARCH_USERNAME", username).
		WithEnvVariable("OPENSEARCH_PASSWORD", password)

	return dag.Golang(h.Src, dagger.GolangOpts{Base: goContainer}).Test(dagger.GolangTestOpts{
		Short:         short,
		Shuffle:       shuffle,
		Run:           run,
		Skip:          skip,
		WithGotestsum: true,
		Path:          path,
	}), nil
}

// Build permit to build project
func (h *Opensearchtools) Build(
	ctx context.Context,
) *dagger.Directory {
	return h.GolangModule.Build()
}

func (h *Opensearchtools) CodeCov(
	ctx context.Context,

	// Optional directory
	// +optional
	src *dagger.Directory,

	// The Codecov token
	// +required
	token *dagger.Secret,
) (stdout string, err error) {
	if src == nil {
		src = h.Src
	}

	return dag.Codecov().Upload(
		ctx,
		src,
		token,
		dagger.CodecovUploadOpts{
			Files: []string{"coverage.out"},
		},
	)
}

// Build permit to build project
func (h *Opensearchtools) BuildImage(
	ctx context.Context,

	// Set tru if you are on CI
	// +default=false
	ci bool,

	// The image version to publish
	// +optional
	version string,

	// The registry username
	// +optional
	registryUsername *dagger.Secret,

	// The registry password
	// +optional
	registryPassword *dagger.Secret,
) (err error) {
	// lint Dockerfile
	_, err = dag.Image().Lint(ctx, h.Src)
	if err != nil {
		return errors.Wrap(err, "Error when run image Lint")
	}

	imageBuilder := dag.Image().Build(h.Src)

	if ci {
		_, err = imageBuilder.Push(ctx, repository, version, registry, dagger.ImageBuildPushOpts{WithRegistryUsername: registryUsername, WithRegistryPassword: registryPassword})
		if err != nil {
			return errors.Wrapf(err, "Error when push image '%s'", repository)
		}

	} else {
		// Force build image
		if _, err = imageBuilder.GetContainer().WithoutEntrypoint().WithExec(helper.ForgeCommand("echo test")).Stdout(ctx); err != nil {
			return errors.Wrapf(err, "Error when build image '%s'", repository)
		}
	}

	return nil
}
