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
// MULTISET is EXACTLY expected. It takes a graphdump.Reader so the comparison
// logic is unit-testable against an in-memory fake with no Bolt/Docker
// dependency, mirroring graphdump.Canonicalize's own testability contract.
//
// The comparison is by multiplicity, not set membership: the command promises
// an exact edge COUNT, so an edge materialized more times than the expected-set
// names it (a concurrent-MERGE race or a duplicate writer output) is a
// duplicate MISMATCH, never silently collapsed to one — a deterministic
// duplicate that a plain set comparison, and the cross-worker digest, would
// both miss.
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
	// expectedCounts tracks per-key multiplicity, not just presence: the
	// command promises an exact edge COUNT, so two identical expected edges (a
	// mis-authored fixture) and two identical graph edges (a duplicate-writer /
	// concurrent-MERGE regression) must both be visible, never collapsed to a
	// set. In practice the reducer dedups its edge rows (seenEdges) and the
	// hand-derived expected-set names each edge once, so any key with an actual
	// count above its expected count is a real duplicate-edge defect.
	expectedCounts := make(map[string]int, len(expected))
	for _, e := range expected {
		expectedCounts[e.Key()]++
	}

	actualCounts := make(map[string]int)
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
		actualCounts[ifa.ExpectedEdge{RelationshipType: edge.Type, SourceEntityID: fromUID, TargetEntityID: toUID}.Key()]++
		return nil
	})
	if err != nil {
		return fmt.Errorf("ifa assert-edges: stream %s edges: %w", domain, err)
	}

	var missing, extra, duplicate []string
	for key, want := range expectedCounts {
		got := actualCounts[key]
		switch {
		case got == 0:
			missing = append(missing, key)
		case got > want:
			// Present, but materialized more times than expected: a duplicate.
			duplicate = append(duplicate, fmt.Sprintf("%s (graph=%d, expected=%d)", key, got, want))
		}
	}
	for key, got := range actualCounts {
		if _, ok := expectedCounts[key]; !ok {
			// Not in the expected set at all. Report the count so a spurious
			// duplicate of an unexpected edge is not undercounted either.
			if got > 1 {
				extra = append(extra, fmt.Sprintf("%s (x%d)", key, got))
			} else {
				extra = append(extra, key)
			}
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(duplicate)
	sort.Strings(endpointErrs)

	if len(missing) == 0 && len(extra) == 0 && len(duplicate) == 0 && len(endpointErrs) == 0 {
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
	if len(duplicate) > 0 {
		fmt.Fprintf(&b, "\n  duplicate (%d, materialized more times than expected — a concurrent-MERGE race or duplicate writer output):", len(duplicate))
		for _, k := range duplicate {
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
