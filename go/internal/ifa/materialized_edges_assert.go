// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ExpectedEdge is the exported identity triple, plus any relationship-MERGE
// identity properties, of one hand-derived expected materialized graph edge
// — the shape `cmd/ifa`'s `assert-edges` verb compares a live graph against.
// It mirrors the unexported sqlRelationshipExpectedEdge the pure vacuity
// guard uses, exported so the live-gate verb reads the SAME expected-edge-set
// fixture through one loader rather than re-declaring the JSON shape.
type ExpectedEdge struct {
	// RelationshipType is the graph relationship type (e.g. "QUERIES_TABLE").
	RelationshipType string
	// SourceEntityID is the edge source node's canonical identity (its uid, or its id for id-keyed labels).
	SourceEntityID string
	// TargetEntityID is the edge target node's canonical identity (its uid, or its id for id-keyed labels).
	TargetEntityID string
	// Identity carries the relationship-property values that participate in
	// this edge's MERGE identity, keyed by property name (e.g. "pattern",
	// "source_path" for a DECLARES_CODEOWNER edge). It is empty for every
	// family whose writer MERGEs on its two endpoint nodes alone —
	// cypher.MaterializedEdgeIdentityProperties names which relationship
	// types require it. LoadExpectedEdges validates that a loaded edge's key
	// set exactly matches its family's declaration.
	Identity map[string]string `json:"identity,omitempty"`
}

// Key is the canonical set-membership key for exact-set comparison: the same
// `type|source|target` shape sqlRelationshipEdgeKey builds, so a live-graph
// edge and a fixture edge collide iff they are the same edge. When Identity
// is empty the result is BYTE-IDENTICAL to the pre-Identity Key() — this is
// what spares every already-proven family with no relationship-property
// identity (sql_relationships, code_calls, ...) from re-proof.
//
// When Identity is non-empty, Key() switches to a fully injective,
// length-prefixed (netstring-style) encoding of EVERY component --
// RelationshipType, SourceEntityID, TargetEntityID, then each Identity
// property in SORTED KEY ORDER -- rather than the raw "|"-joined shape the
// empty-Identity path keeps. A raw join is not injective: nothing stops
// SourceEntityID, TargetEntityID, or an Identity value from containing the
// "|" or "=" delimiters themselves, so two structurally different edges can
// render the identical string. Concretely, a target ending in "|path=P"
// combined with an identity value of "Q" collided with a plain target
// combined with an identity value of "P|path=Q" -- both rendered
// "...T|path=P|path=Q" (see TestExpectedEdgeKeyIsInjective). Length-prefixing
// each field as "<byte length>:<content>" removes the ambiguity regardless
// of what bytes a component holds: a decoder never needs to search for a
// delimiter inside a field's content, so no field's content can be
// misread as a boundary. This only runs on the non-empty-Identity path (the
// two currently gated families, codeowners_ownership_edges and
// submodule_pin_edges), so it cannot affect the byte-identical guarantee for
// families with no declared identity.
func (e ExpectedEdge) Key() string {
	if len(e.Identity) == 0 {
		return sqlRelationshipEdgeKey(e.RelationshipType, e.SourceEntityID, e.TargetEntityID)
	}
	keys := make([]string, 0, len(e.Identity))
	for k := range e.Identity {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	writeLengthPrefixedField(&b, e.RelationshipType)
	writeLengthPrefixedField(&b, e.SourceEntityID)
	writeLengthPrefixedField(&b, e.TargetEntityID)
	for _, k := range keys {
		writeLengthPrefixedField(&b, k)
		writeLengthPrefixedField(&b, e.Identity[k])
	}
	return b.String()
}

// writeLengthPrefixedField appends one netstring-style "<len>:<content>"
// field to b, using s's byte length (not rune count) so the announced length
// matches exactly how many bytes the decoder would need to consume -- the
// injectivity property Key() relies on for its Identity-bearing keys.
// strconv.Itoa plus direct Builder writes, not fmt.Fprintf: profiling showed
// fmt's reflection-based formatting roughly quadrupling this path's cost
// (BenchmarkExpectedEdgeKey, go/internal/ifa/materialized_edges_assert_test.go).
func writeLengthPrefixedField(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
}

// LoadExpectedEdges reads a hand-derived expected-edge-set fixture file (the
// same format the pure vacuity guard consumes) into the exported ExpectedEdge
// shape, validated against family's declared relationship-identity
// properties (cypher.MaterializedEdgeIdentityProperties). It is the single
// loader `cmd/ifa`'s `assert-edges` verb uses, so the live gate and the pure
// `go test` guard can never drift on the fixture format.
//
// Every loaded edge must have a non-empty RelationshipType, SourceEntityID,
// and TargetEntityID, and its Identity key set must equal EXACTLY the set
// family declares for that relationship type — a missing declared key, an
// undeclared key, or any Identity entry on a type with no declared identity
// is a fixture error, not a silently-accepted edge.
func LoadExpectedEdges(path, family string) ([]ExpectedEdge, error) {
	identity, err := cypher.MaterializedEdgeIdentityProperties(family)
	if err != nil {
		return nil, fmt.Errorf("ifa: load expected edges for family %q: %w", family, err)
	}
	edges, err := loadSQLRelationshipExpectedEdges(path)
	if err != nil {
		return nil, err
	}
	out := make([]ExpectedEdge, len(edges))
	for i, e := range edges {
		if strings.TrimSpace(e.RelationshipType) == "" || strings.TrimSpace(e.SourceEntityID) == "" || strings.TrimSpace(e.TargetEntityID) == "" {
			return nil, fmt.Errorf("ifa: expected edge %d in %s has an empty relationship_type, source_entity_id, or target_entity_id", i, path)
		}
		if err := validateExpectedEdgeIdentity(e.Identity, identity[e.RelationshipType]); err != nil {
			return nil, fmt.Errorf("ifa: expected edge %d (%s) in %s: %w", i, e.RelationshipType, path, err)
		}
		// Direct struct conversion: ExpectedEdge and sqlRelationshipExpectedEdge
		// have identical field types and order (only the JSON tags differ, which
		// a conversion ignores), so this stays correct if either grows a field —
		// the compiler rejects the conversion the moment their shapes diverge.
		out[i] = ExpectedEdge(e)
	}
	return out, nil
}

// validateExpectedEdgeIdentity asserts identity's key set equals declared
// exactly, AND that every declared key's value is non-blank. declared is nil
// for a relationship type with no identity properties, in which case any
// non-empty identity is an error — the declared-empty case for a family that
// MERGEs on endpoints alone.
//
// The blank-value check exists because key-set equality alone lets a fixture
// carry a declared key with an empty or whitespace-only value
// (`"pattern": ""`) and still load clean: the key is present, so it is
// neither "missing" nor "undeclared". A fixture authored that way would
// match a graph whose codeowners extractor had itself regressed to emitting
// blank patterns, exactly the silent-collapse class this precursor exists to
// catch — see the sibling check in assertMaterializedEdges
// (go/cmd/ifa/assert_edges.go) for the live-graph half of this guard.
func validateExpectedEdgeIdentity(identity map[string]string, declared []string) error {
	declaredSet := make(map[string]struct{}, len(declared))
	for _, k := range declared {
		declaredSet[k] = struct{}{}
	}
	var undeclared, blank []string
	for k, v := range identity {
		if _, ok := declaredSet[k]; !ok {
			undeclared = append(undeclared, k)
			continue
		}
		if strings.TrimSpace(v) == "" {
			blank = append(blank, k)
		}
	}
	var missing []string
	for _, k := range declared {
		if _, ok := identity[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(missing)
	sort.Strings(blank)
	if len(undeclared) > 0 || len(missing) > 0 || len(blank) > 0 {
		return fmt.Errorf("identity keys %v do not match the declared set %v (undeclared: %v, missing: %v, blank: %v)", identityKeys(identity), declared, undeclared, missing, blank)
	}
	return nil
}

// identityKeys renders an Identity map's keys as a sorted slice for error
// messages.
func identityKeys(identity map[string]string) []string {
	out := make([]string, 0, len(identity))
	for k := range identity {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// edgeTypeSet projects a writer registry's type->reason map onto the set shape
// the assert path compares against. Keeping the projection in one helper is what
// lets a family's arm be a single line, so adding a family cannot accidentally
// ship a subtly different (for example, reason-keyed) set.
func edgeTypeSet(registry map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(registry))
	for edgeType := range registry {
		out[edgeType] = struct{}{}
	}
	return out
}

// MaterializedEdgeDomainEdgeTypes returns the set of graph relationship types
// the given materialized-edge family's writer registry accepts, so a live
// `assert-edges` check knows which of a graph's edges belong to the family it
// is asserting (and ignores every unrelated edge type — CONTAINS, the GCP
// families, etc. — that shares the same graph).
//
// Every set is registry-derived (#5330 pattern), never hand-listed here, so an
// edge type added to a writer registry is asserted without a second edit. The
// multi-type families resolve through explicit arms because their sets span
// several templates and files: sql_relationships, code_calls (CALLS,
// REFERENCES, USES_METACLASS, INSTANTIATES), inheritance_edges (INHERITS,
// OVERRIDES, ALIASES, IMPLEMENTS) and repo_dependency (six repo-to-repo types
// plus RUNS_ON). The remaining families each materialize a single type and
// resolve from cypher's shared table.
//
// An unknown family returns an error rather than an empty set, so the caller
// fails closed: an empty type set would assert nothing and let any graph pass
// vacuously, which is exactly the false green the #5543 waiver rows exist to
// prevent.
func MaterializedEdgeDomainEdgeTypes(domain string) (map[string]struct{}, error) {
	switch domain {
	case "sql_relationships":
		return edgeTypeSet(cypher.SQLRelationshipMaterializedEdgeTypes()), nil
	case "code_calls":
		return edgeTypeSet(cypher.CodeCallMaterializedEdgeTypes()), nil
	case "inheritance_edges":
		return edgeTypeSet(cypher.InheritanceMaterializedEdgeTypes()), nil
	case "repo_dependency":
		return edgeTypeSet(cypher.RepoDependencyMaterializedEdgeTypes()), nil
	default:
		// The single-relationship-type families resolve from the shared registry
		// table. They are looked up rather than switched on so registering one is
		// a data change on the writer side, while the multi-type families above
		// keep explicit arms because their sets span several templates.
		if reg, ok := cypher.SingleTypeMaterializedEdgeTypes(domain); ok {
			return edgeTypeSet(reg), nil
		}
		return nil, fmt.Errorf("ifa: no materialized-edge family registered for domain %q; register its edge types beside its writer (multi-type) or in cypher.singleTypeMaterializedEdgeFamilies (single-type) before proving it on a live gate", domain)
	}
}
