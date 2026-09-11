// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

var openAssertGCPProjectEdgeScopesReader = func(ctx context.Context) (graphdump.Reader, func(), error) {
	return openBoltGraphReader(ctx, os.Getenv)
}

func runAssertGCPProjectEdgeScopesCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("ifa assert-gcp-project-edge-scopes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	synthSeed := flags.Int("synth-seed", 4580, "seed used to generate the Ifa multi-project GCP fixture")
	synthProjects := flags.Int("synth-projects", 8, "project count in the Ifa multi-project GCP fixture")
	synthResources := flags.Int("synth-resources", 64, "resources per project in the Ifa multi-project GCP fixture")
	if err := flags.Parse(args); err != nil {
		return err //nolint:wrapcheck // flag errors are self-describing.
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("ifa assert-gcp-project-edge-scopes: unexpected argument %q", flags.Arg(0))
	}
	expected, err := ifaGCPProjectScopeExpectations(*synthSeed, *synthProjects, *synthResources)
	if err != nil {
		return fmt.Errorf("ifa assert-gcp-project-edge-scopes: %w", err)
	}

	reader, closeFn, err := openAssertGCPProjectEdgeScopesReader(ctx)
	if err != nil {
		return fmt.Errorf("ifa assert-gcp-project-edge-scopes: open graph backend: %w", err)
	}
	defer closeFn()

	checked, err := graphdump.ValidateGCPProjectEdgeScopes(ctx, reader, expected)
	if err != nil {
		return fmt.Errorf("ifa assert-gcp-project-edge-scopes: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "ifa assert-gcp-project-edge-scopes: checked=%d cross_scope=0\n", checked)
	return nil
}

func ifaGCPProjectScopeExpectations(seed, projects, resources int) (map[string]graphdump.GCPProjectScopeExpectation, error) {
	if projects < 1 {
		return nil, fmt.Errorf("synth-projects must be positive")
	}
	if resources < 2 {
		return nil, fmt.Errorf("synth-resources must be at least 2 for a non-vacuous relationship fixture")
	}

	expected := make(map[string]graphdump.GCPProjectScopeExpectation, projects+1)
	for index := range projects {
		projectID := fmt.Sprintf("acme-demo-gcp-%02d", index)
		scopeID := fmt.Sprintf("gcp:project:%s:seed:%d", projectID, seed)
		expected[scopeID] = graphdump.GCPProjectScopeExpectation{ProjectID: projectID, EdgeCount: resources - 1}
	}
	expected["gcp:project:supply-chain-demo-project"] = graphdump.GCPProjectScopeExpectation{
		ProjectID: "supply-chain-demo-project",
		EdgeCount: 123,
	}
	return expected, nil
}
