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
	OpensearchVersion string = "2.18.0"
	username          string = "admin"
	password          string = "vLPeJYa8.3RqtZCcAK6jNz"
	mockgenVersion           = "v0.3.0"
	gitUsername       string = "ci"
	gitEmail          string = "ci@localhost"
	defaultGitBranch  string = "2.x"
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
	reportFile := h.Test(ctx)
	dir = dir.WithFile("coverage.out", reportFile)

	if ci {
		if codeCoveToken == nil {
			return nil, errors.New("You need to provide CodeCov token")
		}
		stdout, err = h.CodeCov(ctx, dir, codeCoveToken)
		if err != nil {
			return nil, errors.Wrapf(err, "Error when upload report on CodeCov: %s", stdout)
		}

		// Create release on github with gorelease
		if version != defaultGitBranch {

			githubToken, err := gitToken.Plaintext(ctx)
			if err != nil {
				return nil, errors.Wrap(err, "Error when get Github token")
			}

			if _, err = dag.Goreleaser().WithSource(h.Src).Release(ctx, dagger.GoreleaserReleaseOpts{
				Clean:        true,
				Cfg:          ".goreleaser.yml",
				EnvVars:      []string{fmt.Sprintf("GITHUB_TOKEN=%s", githubToken)},
				AutoSnapshot: true,
			}); err != nil {
				return nil, errors.Wrap(err, "Error when call Gorelease")
			}
		}

		if _, err = dag.Git().SetConfig(gitUsername, gitEmail, dagger.GitSetConfigOpts{BaseRepoURL: "github.com", Token: gitToken}).SetRepo(dir, dagger.GitSetRepoOpts{Branch: defaultGitBranch}).CommitAndPush(ctx, "Commit from CI. skip ci"); err != nil {
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

// Test permit to run tests
func (h *Opensearchtools) Test(
	ctx context.Context,
) *dagger.File {
	// Run Opensearch
	opensearchService := dag.Container().
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

	return h.GolangModule.Container().
		WithServiceBinding("opensearch.svc", opensearchService).
		WithExec(helper.ForgeScript(`
set -e
sleep 10
curl --fail -XGET -k -u %s:%s "https://opensearch.svc:9200/_cluster/health?wait_for_status=yellow&timeout=500s"
curl --fail -u %s:%s -k -H "Content-Type: application/x-ndjson" -XPOST https://opensearch.svc:9200/logs/_bulk?pretty --data-binary @fixtures/logs/bulk.ndjson
OPENSEARCH_USERNAME=%s OPENSEARCH_PASSWORD=%s go test ./... -v -count 1 -parallel 1 -race -coverprofile=coverage.out -covermode=atomic -timeout 120m
		`, username, password, username, password, username, password)).
		File("coverage.out")
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
			Files:   []string{"coverage.out"},
			Verbose: true,
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
