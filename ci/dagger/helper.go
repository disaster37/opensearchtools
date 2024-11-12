package main

import (
	"context"
	"fmt"

	"dagger/opensearchtools/internal/dagger"

	"golang.org/x/mod/modfile"
)

const (
	// Prior to go 1.21, the go.mod doesn't include the full version, so build
	// against the latest possible version
	go1_17 = "golang:1.17.13-bullseye"
	go1_18 = "golang:1.18.10-bullseye"
	go1_19 = "golang:1.19.13-bullseye"
	go1_20 = "golang:1.20.13-bookworm"

	goMod     = "go.mod"
	goWorkDir = "/src"
)

func inspectModVersion(ctx context.Context, src *dagger.Directory) (string, error) {
	mod, err := src.File(goMod).Contents(ctx)
	if err != nil {
		return "", err
	}

	f, err := modfile.Parse(goMod, []byte(mod), nil)
	if err != nil {
		return "", err
	}
	return f.Go.Version, nil
}

func defaultImage(version string) *dagger.Container {
	var image string
	switch version {
	case "1.17":
		image = go1_17
	case "1.18":
		image = go1_18
	case "1.19":
		image = go1_19
	case "1.20":
		image = go1_20
	default:
		image = fmt.Sprintf("golang:%s-bookworm", version)
	}

	return dag.Container().From(image)
}

func mountCaches(ctx context.Context, base *dagger.Container) *dagger.Container {
	goCacheEnv, _ := base.WithExec([]string{"go", "env", "GOCACHE"}).Stdout(ctx)
	goModCacheEnv, _ := base.WithExec([]string{"go", "env", "GOMODCACHE"}).Stdout(ctx)

	gomod := dag.CacheVolume("gomod")
	gobuild := dag.CacheVolume("gobuild")

	return base.
		WithMountedCache(goModCacheEnv, gomod).
		WithMountedCache(goCacheEnv, gobuild)
}
