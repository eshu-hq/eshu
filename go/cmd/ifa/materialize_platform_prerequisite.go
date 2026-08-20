// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const (
	platformPrerequisiteRepositoryCountCypher = `MATCH (repo:Repository {id: $id}) RETURN count(repo) AS count`
	platformPrerequisitePlatformCountCypher   = `MATCH (platform:Platform {id: $id}) RETURN count(platform) AS count`
)

type platformPrerequisiteOptions struct {
	repoID      string
	kind        string
	name        string
	locator     string
	provider    string
	environment string
	region      string
}

type platformPrerequisiteMaterializer interface {
	Materialize(context.Context, []reducer.InfrastructurePlatformRow) (reducer.InfrastructurePlatformResult, error)
}

type platformPrerequisiteVerifier interface {
	CountRepository(context.Context, string) (int, error)
	CountPlatform(context.Context, string) (int, error)
}

type platformPrerequisiteBackend interface {
	reducer.CypherExecutor
	platformPrerequisiteVerifier
}

var openPlatformPrerequisiteBackend = func(ctx context.Context) (platformPrerequisiteBackend, func(), error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = driver.Close(context.Background()) }
	return &boltPlatformPrerequisiteBackend{driver: driver, db: cfg.DatabaseName}, closeFn, nil
}

func parseMaterializePlatformPrerequisiteFlags(args []string, stderr io.Writer) (platformPrerequisiteOptions, error) {
	fs := flag.NewFlagSet("ifa materialize-platform-prerequisite", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var options platformPrerequisiteOptions
	fs.StringVar(&options.repoID, "repo-id", "", "source Repository id")
	fs.StringVar(&options.kind, "kind", "", "Platform kind")
	fs.StringVar(&options.name, "name", "", "Platform name")
	fs.StringVar(&options.locator, "locator", "", "Platform locator")
	fs.StringVar(&options.provider, "provider", "", "Platform provider")
	fs.StringVar(&options.environment, "environment", "", "Platform environment")
	fs.StringVar(&options.region, "region", "", "Platform region")
	if err := fs.Parse(args); err != nil {
		return platformPrerequisiteOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if fs.NArg() != 0 {
		return platformPrerequisiteOptions{}, fmt.Errorf("ifa materialize-platform-prerequisite: positional arguments are not accepted")
	}

	seen := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	required := []struct {
		name  string
		value *string
	}{
		{name: "repo-id", value: &options.repoID},
		{name: "kind", value: &options.kind},
		{name: "name", value: &options.name},
		{name: "locator", value: &options.locator},
	}
	for _, input := range required {
		*input.value = strings.TrimSpace(*input.value)
		if *input.value == "" {
			return platformPrerequisiteOptions{}, fmt.Errorf("ifa materialize-platform-prerequisite: -%s is required", input.name)
		}
	}
	optional := []struct {
		name  string
		value *string
	}{
		{name: "provider", value: &options.provider},
		{name: "environment", value: &options.environment},
		{name: "region", value: &options.region},
	}
	for _, input := range optional {
		*input.value = strings.TrimSpace(*input.value)
		if seen[input.name] && *input.value == "" {
			return platformPrerequisiteOptions{}, fmt.Errorf("ifa materialize-platform-prerequisite: -%s must not be blank when set", input.name)
		}
	}
	return options, nil
}

func runMaterializePlatformPrerequisiteCommand(
	ctx context.Context,
	args []string,
	stdout,
	stderr io.Writer,
) error {
	options, err := parseMaterializePlatformPrerequisiteFlags(args, stderr)
	if err != nil {
		return err
	}

	backend, closeFn, err := openPlatformPrerequisiteBackend(ctx)
	if err != nil {
		return fmt.Errorf("ifa materialize-platform-prerequisite: open graph backend: %w", err)
	}
	defer closeFn()

	platformID, count, err := materializePlatformPrerequisite(
		ctx,
		options,
		reducer.NewInfrastructurePlatformMaterializer(backend),
		backend,
	)
	if err != nil {
		return fmt.Errorf("ifa materialize-platform-prerequisite: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "platform_id=%s verified=%d\n", platformID, count)
	return nil
}

func materializePlatformPrerequisite(
	ctx context.Context,
	options platformPrerequisiteOptions,
	materializer platformPrerequisiteMaterializer,
	verifier platformPrerequisiteVerifier,
) (string, int, error) {
	platformID := reducer.CanonicalPlatformID(reducer.CanonicalPlatformInput{
		Kind:        options.kind,
		Provider:    options.provider,
		Name:        options.name,
		Environment: options.environment,
		Region:      options.region,
		Locator:     options.locator,
	})
	if platformID == "" {
		return "", 0, fmt.Errorf("derive canonical Platform id: insufficient identity input")
	}

	repositoryCount, err := verifier.CountRepository(ctx, options.repoID)
	if err != nil {
		return "", 0, fmt.Errorf("verify source Repository %q: %w", options.repoID, err)
	}
	if repositoryCount != 1 {
		return "", 0, fmt.Errorf("source Repository %q prerequisite count = %d, want 1", options.repoID, repositoryCount)
	}

	result, err := materializer.Materialize(ctx, []reducer.InfrastructurePlatformRow{{
		RepoID:              options.repoID,
		PlatformID:          platformID,
		PlatformName:        options.name,
		PlatformKind:        options.kind,
		PlatformProvider:    options.provider,
		PlatformEnvironment: options.environment,
		PlatformRegion:      options.region,
		PlatformLocator:     options.locator,
	}})
	if err != nil {
		return "", 0, fmt.Errorf("materialize Platform prerequisite: %w", err)
	}
	if result.PlatformEdgesWritten != 1 {
		return "", 0, fmt.Errorf("materializer reported %d rows, want 1", result.PlatformEdgesWritten)
	}

	platformCount, err := verifier.CountPlatform(ctx, platformID)
	if err != nil {
		return "", 0, fmt.Errorf("verify Platform %q: %w", platformID, err)
	}
	if platformCount != 1 {
		return "", 0, fmt.Errorf("verified %d Platform nodes for %q, want 1", platformCount, platformID)
	}
	return platformID, platformCount, nil
}

type boltPlatformPrerequisiteBackend struct {
	driver neo4j.DriverWithContext
	db     string
}

func (b *boltPlatformPrerequisiteBackend) ExecuteCypher(
	ctx context.Context,
	cypher string,
	params map[string]any,
) error {
	result, err := neo4j.ExecuteQuery(
		ctx,
		b.driver,
		cypher,
		params,
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(b.db),
	)
	if err != nil {
		return fmt.Errorf("execute platform prerequisite write: %w", err)
	}
	if result == nil {
		return fmt.Errorf("execute platform prerequisite write: nil result")
	}
	return nil
}

func (b *boltPlatformPrerequisiteBackend) CountRepository(ctx context.Context, repoID string) (int, error) {
	return b.countByID(ctx, platformPrerequisiteRepositoryCountCypher, repoID)
}

func (b *boltPlatformPrerequisiteBackend) CountPlatform(ctx context.Context, platformID string) (int, error) {
	return b.countByID(ctx, platformPrerequisitePlatformCountCypher, platformID)
}

func (b *boltPlatformPrerequisiteBackend) countByID(ctx context.Context, cypher, id string) (int, error) {
	result, err := neo4j.ExecuteQuery(
		ctx,
		b.driver,
		cypher,
		map[string]any{"id": id},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(b.db),
	)
	if err != nil {
		return 0, fmt.Errorf("execute prerequisite count: %w", err)
	}
	if result == nil {
		return 0, fmt.Errorf("prerequisite count returned a nil result")
	}
	if len(result.Records) != 1 {
		return 0, fmt.Errorf("prerequisite count returned %d rows, want 1", len(result.Records))
	}
	count, _, err := neo4j.GetRecordValue[int64](result.Records[0], "count")
	if err != nil {
		return 0, fmt.Errorf("read prerequisite count: %w", err)
	}
	return int(count), nil
}

var _ platformPrerequisiteBackend = (*boltPlatformPrerequisiteBackend)(nil)
