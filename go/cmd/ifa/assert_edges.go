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
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
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
	expected, err := ifa.LoadExpectedEdges(o.expected, o.domain)
	if err != nil {
		return fmt.Errorf("ifa assert-edges: %w", err)
	}

	reader, closeFn, err := openBoltGraphReader(ctx, os.Getenv)
	if err != nil {
		return fmt.Errorf("ifa assert-edges: open graph backend: %w", err)
	}
	defer closeFn()

	// Absent constraints are the common case and mean "match by type alone".
	// MaterializedEdgeEndpointLabels returns a nil map with ok=false there, and a
	// nil map's lookups report not-constrained, so the filter is a no-op rather
	// than a silent match-nothing.
	endpoints, _ := cypher.MaterializedEdgeEndpointLabels(o.domain)

	if err := assertMaterializedEdges(ctx, reader, o.domain, edgeTypes, endpoints, expected); err != nil {
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
// An edge's endpoint identity (endpointID) is its node's "uid" property when it has one, and
// its "id" otherwise — the graph keys nodes both ways and the assertion has to
// speak both. Content entities are uid-keyed (for a SQL entity the uid equals
// its content_entity id; for a canonicalNamePathLineEntityLabels endpoint such
// as a Function it is the derived hash the fixture precomputes — see
// internal/ifa's sqlFamilyGetUserFunctionUID), while Repository, Workload,
// WorkloadInstance and Platform are MERGEd `{id: ...}` and carry no uid at all.
// uid is consulted first, so a node carrying both resolves by uid. An edge whose
// endpoint has NEITHER is a real defect (an unmaterialized endpoint node), so it
// is surfaced, never silently skipped.
//
// Endpoint scoping (#5543): a family whose relationship types are shared with
// another family also constrains its edges' endpoint labels. DEPENDS_ON is
// written Repository->Repository by repo_dependency and Workload->Workload by
// workload_dependency, so a type-only filter makes each family count the
// other's edges as spurious extras once both are proven in one live cell.
//
// Absent constraints mean "match every edge of this family's types", never
// "match nothing" — the latter would assert an empty population and pass any
// graph.
func assertMaterializedEdges(
	ctx context.Context,
	reader graphdump.Reader,
	domain string,
	edgeTypes map[string]struct{},
	endpoints map[string]cypher.MaterializedEdgeEndpoint,
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
		// A constrained type must match its endpoint labels too. Only families
		// with a proven type collision carry constraints, and the cypher-side
		// guard requires them to be total over the family's registered types, so
		// a constrained family can never have a type silently fall through here
		// unmatched.
		if endpoint, constrained := endpoints[edge.Type]; constrained {
			if !hasLabel(edge.FromLabels, endpoint.FromLabel) || !hasLabel(edge.ToLabels, endpoint.ToLabel) {
				return nil
			}
			// Provenance, where the family declares it. Two live writers can emit
			// the same type between the same labels (RUNS_ON), and only the
			// evidence_source the writer stamped tells them apart — the same
			// property the family's retract scopes on.
			if endpoint.EvidenceSource != "" {
				if got, _ := edge.Props["evidence_source"].(string); got != endpoint.EvidenceSource {
					return nil
				}
			}
		}
		fromUID := endpointID(edge.FromProps)
		toUID := endpointID(edge.ToProps)
		if fromUID == "" || toUID == "" {
			// Name WHICH side is unidentified. This branch fires when either
			// endpoint lacks an identity, so a message asserting both are missing
			// sends the reader looking at a node that is materialized correctly.
			missing := "source and target"
			switch {
			case fromUID == "" && toUID != "":
				missing = "source"
			case toUID == "" && fromUID != "":
				missing = "target"
			}
			endpointErrs = append(endpointErrs, fmt.Sprintf(
				"%s edge whose %s endpoint carries neither uid nor id (from=%q to=%q) — an unmaterialized endpoint node",
				edge.Type, missing, fromUID, toUID,
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

// endpointID extracts a node's canonical identity: its "uid" when present, and
// its "id" otherwise. It returns "" when neither is present as a string, which
// the caller reports as an unmaterialized endpoint.
func endpointID(props map[string]any) string {
	if props == nil {
		return ""
	}
	if uid, ok := props["uid"].(string); ok && uid != "" {
		return uid
	}
	// Fall back to "id": the graph keys node labels two different ways and the
	// assert path has to speak both. Content entities (Function, Class, File,
	// the SQL family's endpoints) are uid-keyed, but Repository, Workload,
	// WorkloadInstance and Platform are MERGEd `{id: ...}` and carry no uid at
	// all (canonical_node_cypher.go:98, canonical.go:24/36/50).
	//
	// Reading only "uid" made every repo_dependency and workload_dependency edge
	// look like it had an unmaterialized endpoint, which is both wrong and a
	// misleading diagnosis — the node exists, it is simply keyed by id. uid stays
	// first so uid-bearing endpoints are unaffected.
	id, _ := props["id"].(string)
	return id
}

// hasLabel reports whether a graph endpoint carries the required node label.
//
// Endpoints carry a handful of labels in practice (1-3 in this graph), and this
// runs once per edge per gate run, so the linear scan is deliberate — an index or
// set conversion would cost more to build than it saves.
//
// Endpoints commonly carry several labels, so this is membership rather than
// equality. An empty required label would match nothing and silently drop the
// edge type from the assertion; the cypher-side guard rejects blank labels so
// that cannot reach here, and this returns false rather than true to keep the
// failure loud if it ever does.
func hasLabel(labels []string, required string) bool {
	if required == "" {
		return false
	}
	for _, label := range labels {
		if label == required {
			return true
		}
	}
	return false
}
