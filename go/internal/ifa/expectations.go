// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// readSurfaceManifestPrefix is the replay-coverage-manifest surface key prefix
// for a fact-kind-registry read surface (replaycoverage/surfaces.go).
const readSurfaceManifestPrefix = "read_surface:"

// QueryRef points at one replay-coverage-manifest row that proves a fact
// kind's read_surface query truth. It is a read-only projection of
// replaycoverage.CoverageEntry: Ifá never re-declares query coverage, it reads
// the manifest replay-coverage-gate (#4173) already reconciles.
type QueryRef struct {
	// Scenario is the replay-coverage-manifest scenario artifact kind (e.g.
	// api_mcp_golden) that proves the read surface.
	Scenario replaycoverage.ScenarioType
	// Ref is the scenario artifact ref (a B-12 query-shape key for
	// api_mcp_golden/cli_golden rows).
	Ref string
	// ProofGate names the CI gate that proves the scenario green.
	ProofGate string
}

// KindExpectation is the derived P1 expectation for one fact-kind-registry
// entry: its query-truth binding (1a) and its payload-schema derivation (1c).
// Graph-evidence reach (1b/1d) is proven by running the real
// relationships.DiscoverEvidence extractor (see EvidenceSatisfies), not carried
// as a per-kind field here. Nothing here is hand-listed; every field is
// computed from the registry entry and the replay-coverage manifest.
type KindExpectation struct {
	// Kind is the fact-kind-registry Kind string.
	Kind string
	// ReadSurface is the registry entry's ReadSurface, normalized exactly like
	// replaycoverage/surfaces.go's EnumerateSupported: trimmed, with a blank or
	// case-insensitive "none" value collapsed to "".
	ReadSurface string
	// QueryRefs are the replay-coverage-manifest read_surface:* rows matching
	// ReadSurface. Empty when ReadSurface is "" or no manifest row names it
	// (an honest derivation gap, not an error).
	QueryRefs []QueryRef
	// PayloadSchema is the registry entry's PayloadSchema path, carried through
	// unchanged so callers can locate the same fixturepack schema Ifá validates
	// against.
	PayloadSchema string
	// RegistryOnly is true when PayloadSchema is blank: the kind has no typed
	// payload-schema artifact yet, so Ifá can only assert the kind's presence in
	// the registry, never validate its payload shape. Non-blocking.
	RegistryOnly bool
}

// DerivedExpectations is the full P1 derivation join output: one
// KindExpectation per fact-kind-registry entry, plus the B-12 snapshot's
// evidence-narrowed correlations (the rc's an Odù's graph evidence can ever
// satisfy; see EvidenceSatisfies).
type DerivedExpectations struct {
	// Kinds are the derived per-fact-kind expectations, sorted by Kind.
	Kinds []KindExpectation
	// NarrowedCorrelations are the B-12 required correlations that carry a
	// non-empty EvidenceKinds filter (rc-29..rc-36 as of #4394), sorted by ID.
	// Unfiltered correlations (e.g. rc-19, rc-1) stay golden-gate-owned: Ifá
	// cannot isolate one verb's contribution to a shared, tool-agnostic edge
	// type without the evidence-kind filter.
	NarrowedCorrelations []goldengate.RequiredCorrelation
}

// Derive computes DerivedExpectations from the live fact-kind registry, the
// loaded B-12 golden snapshot, and the loaded replay-coverage manifest. It
// performs no I/O itself — callers load each input from its own source of
// truth (facts.FactKindRegistry, goldengate.LoadSnapshot,
// replaycoverage.LoadManifest) — so the derivation stays pure and
// unit-testable against synthetic inputs.
func Derive(entries []facts.FactKindRegistryEntry, snap goldengate.Snapshot, replayManifest replaycoverage.Manifest) (DerivedExpectations, error) {
	queryRefsBySurface := indexReadSurfaceQueryRefs(replayManifest)

	kinds := make([]KindExpectation, 0, len(entries))
	for _, entry := range entries {
		kinds = append(kinds, deriveKindExpectation(entry, queryRefsBySurface))
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i].Kind < kinds[j].Kind })

	var narrowed []goldengate.RequiredCorrelation
	for _, rc := range snap.Graph.RequiredCorrelations {
		if len(rc.EvidenceKinds) > 0 {
			narrowed = append(narrowed, rc)
		}
	}
	sort.Slice(narrowed, func(i, j int) bool { return narrowed[i].ID < narrowed[j].ID })

	return DerivedExpectations{Kinds: kinds, NarrowedCorrelations: narrowed}, nil
}

func deriveKindExpectation(entry facts.FactKindRegistryEntry, queryRefsBySurface map[string][]QueryRef) KindExpectation {
	readSurface := normalizeReadSurface(entry.ReadSurface)
	ke := KindExpectation{
		Kind:          entry.Kind,
		ReadSurface:   readSurface,
		PayloadSchema: strings.TrimSpace(entry.PayloadSchema),
		RegistryOnly:  strings.TrimSpace(entry.PayloadSchema) == "",
	}
	if readSurface != "" {
		ke.QueryRefs = queryRefsBySurface[readSurface]
	}
	return ke
}

// normalizeReadSurface mirrors replaycoverage/surfaces.go's EnumerateSupported
// normalization exactly: trim whitespace, then collapse a blank or
// case-insensitive "none" value to "" (no read surface).
func normalizeReadSurface(readSurface string) string {
	rs := strings.TrimSpace(readSurface)
	if rs == "" || strings.EqualFold(rs, "none") {
		return ""
	}
	return rs
}

// indexReadSurfaceQueryRefs builds the read_surface -> []QueryRef index from
// every "read_surface:<surface>" row in the replay-coverage manifest. Multiple
// coverage entries could in principle map to the same surface; QueryRefs
// preserves manifest order.
func indexReadSurfaceQueryRefs(m replaycoverage.Manifest) map[string][]QueryRef {
	index := map[string][]QueryRef{}
	for _, entry := range m.Coverage {
		surface, ok := strings.CutPrefix(entry.Surface, readSurfaceManifestPrefix)
		if !ok || surface == "" {
			continue
		}
		index[surface] = append(index[surface], QueryRef{
			Scenario:  entry.Scenario,
			Ref:       entry.Ref,
			ProofGate: entry.ProofGate,
		})
	}
	return index
}
