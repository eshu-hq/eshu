// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

// assertEdgesOptions holds the parsed command-line inputs for one
// "ifa assert-edges" run.
type assertEdgesOptions struct {
	domain   string
	expected string
}

func parseAssertEdgesFlags(args []string, stderr io.Writer) (assertEdgesOptions, error) {
	fs := flag.NewFlagSet("ifa assert-edges", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o assertEdgesOptions
	fs.StringVar(&o.domain, "domain", "", "materialized-edge family to assert (e.g. sql_relationships)")
	fs.StringVar(&o.expected, "expected", "", "path to the hand-derived expected-edge-set JSON fixture")
	if err := fs.Parse(args); err != nil {
		return assertEdgesOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if strings.TrimSpace(o.domain) == "" {
		return assertEdgesOptions{}, fmt.Errorf("ifa assert-edges: -domain is required")
	}
	if strings.TrimSpace(o.expected) == "" {
		return assertEdgesOptions{}, fmt.Errorf("ifa assert-edges: -expected is required")
	}
	return o, nil
}

// runAssertEdgesCommand implements `ifa assert-edges`: the Ifá materialized-edge
// exhaustiveness gate's LIVE, set-exact non-vacuity assertion (#5351). It reads
// every edge of the named family's registry types from the live graph via
// graphdump.Reader (the same Bolt read surface `ifa graph-dump` uses) and
// asserts the family's materialized edges are EXACTLY the hand-derived expected
// set — same count, same (relationship_type, source_uid, target_uid) triples.
//
// This is the assertion the P2 determinism digest alone cannot make: digest
// equality across N∈{1,2,4} proves the graph is worker-count-invariant, but a
// family that silently materializes ZERO edges in ALL cells has an identical
// (empty-for-that-family) digest in every cell and passes the digest check
// vacuously. The absolute expected set catches that regression class — exactly
// the silent no-op #5351's own fixture work surfaced (a missing endpoint
// File/Function node makes an edge MATCH a no-op with no error).
//
// The flags are parsed and the expected-edge-set file is loaded before the
// backend is opened: a bad flag or a missing/empty fixture fails fast without a
// graph connection, so a hermetic caller can exercise those paths without
// Docker (mirrors runGraphDumpCommand's flag-before-backend ordering).
func runAssertEdgesCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseAssertEdgesFlags(args, stderr)
	if err != nil {
		return err
	}

	edgeTypes, err := ifa.MaterializedEdgeDomainEdgeTypes(o.domain)
	if err != nil {
		return fmt.Errorf("ifa assert-edges: %w", err)
	}
	expected, err := ifa.LoadExpectedEdges(o.expected)
	if err != nil {
		return fmt.Errorf("ifa assert-edges: %w", err)
	}

	reader, closeFn, err := openBoltGraphReader(ctx, os.Getenv)
	if err != nil {
		return fmt.Errorf("ifa assert-edges: open graph backend: %w", err)
	}
	defer closeFn()

	if err := assertMaterializedEdges(ctx, reader, o.domain, edgeTypes, expected); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "ifa assert-edges: domain=%s expected=%d edges matched exactly\n", o.domain, len(expected))
	return nil
}

// assertMaterializedEdges streams every graph edge, keeps only those whose type
// is in edgeTypes (the family's registry edge types), and asserts the resulting
// set is EXACTLY expected. It takes a graphdump.Reader so the set-comparison
// logic is unit-testable against an in-memory fake with no Bolt/Docker
// dependency, mirroring graphdump.Canonicalize's own testability contract.
//
// An edge's endpoint identity is its node's "uid" property — the canonical
// graph node id the expected-edge-set fixture's source/target_entity_id names
// (for a SQL entity the uid equals its content_entity id; for a
// canonicalNamePathLineEntityLabels endpoint such as a Function it is the
// derived hash the fixture precomputes — see internal/ifa's
// sqlFamilyGetUserFunctionUID). An edge missing a uid on either endpoint is a
// real defect (an unmaterialized endpoint node), so it is surfaced, never
// silently skipped.
func assertMaterializedEdges(
	ctx context.Context,
	reader graphdump.Reader,
	domain string,
	edgeTypes map[string]struct{},
	expected []ifa.ExpectedEdge,
) error {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		expectedSet[e.Key()] = struct{}{}
	}

	actualSet := make(map[string]struct{})
	var endpointErrs []string
	err := reader.StreamEdges(ctx, func(edge graphdump.Edge) error {
		if _, ok := edgeTypes[edge.Type]; !ok {
			return nil
		}
		fromUID := propUID(edge.FromProps)
		toUID := propUID(edge.ToProps)
		if fromUID == "" || toUID == "" {
			endpointErrs = append(endpointErrs, fmt.Sprintf(
				"%s edge with missing endpoint uid (from=%q to=%q) — an unmaterialized endpoint node",
				edge.Type, fromUID, toUID,
			))
			return nil
		}
		actualSet[ifa.ExpectedEdge{RelationshipType: edge.Type, SourceEntityID: fromUID, TargetEntityID: toUID}.Key()] = struct{}{}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ifa assert-edges: stream %s edges: %w", domain, err)
	}

	var missing, extra []string
	for key := range expectedSet {
		if _, ok := actualSet[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range actualSet {
		if _, ok := expectedSet[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(endpointErrs)

	if len(missing) == 0 && len(extra) == 0 && len(endpointErrs) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ifa assert-edges: domain=%s materialized edge set does not match the expected set exactly", domain)
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\n  missing (%d, in expected-set but not in graph — a family silently NOT materializing):", len(missing))
		for _, k := range missing {
			fmt.Fprintf(&b, "\n    %s", k)
		}
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "\n  extra (%d, in graph but not in expected-set — fixture drift or a spurious edge):", len(extra))
		for _, k := range extra {
			fmt.Fprintf(&b, "\n    %s", k)
		}
	}
	if len(endpointErrs) > 0 {
		fmt.Fprintf(&b, "\n  endpoint defects (%d):", len(endpointErrs))
		for _, e := range endpointErrs {
			fmt.Fprintf(&b, "\n    %s", e)
		}
	}
	return fmt.Errorf("%s", b.String())
}

// propUID extracts a node's canonical "uid" property, returning "" when it is
// absent or not a string.
func propUID(props map[string]any) string {
	if props == nil {
		return ""
	}
	uid, _ := props["uid"].(string)
	return uid
}
