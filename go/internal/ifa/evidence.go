// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// RepositoryCatalog derives the repository catalog an Odù's own "repository"
// facts anchor relationships.DiscoverEvidence against. It filters envelopes to
// FactKind=="repository", derives each entry through
// relationships.RepositoryCatalogEntry (the same derivation the Postgres
// streaming commit path uses), and dedupes by RepoID keeping the first
// occurrence — mirroring go/internal/storage/postgres's loadRepositoryCatalog
// exactly, so an Odù's derived catalog matches what the real ingestion
// pipeline would compute from the same facts.
func RepositoryCatalog(envelopes []facts.Envelope) []relationships.CatalogEntry {
	seen := map[string]struct{}{}
	var catalog []relationships.CatalogEntry
	for _, envelope := range envelopes {
		if envelope.FactKind != repositoryFactKind {
			continue
		}
		entry, ok := relationships.RepositoryCatalogEntry(envelope.Payload)
		if !ok {
			continue
		}
		if _, dup := seen[entry.RepoID]; dup {
			continue
		}
		seen[entry.RepoID] = struct{}{}
		catalog = append(catalog, entry)
	}
	return catalog
}

// DiscoveredEvidence runs the production relationship-evidence extractor
// (relationships.DiscoverEvidence) over odu's own facts, anchored by the
// catalog RepositoryCatalog derives from odu's own repository facts. This is
// the graph axis of the P1 derivation join (design §1b): it proves an Odù's
// graph truth by running the real extractor, never by asserting a hand-built
// evidence-kind table.
func DiscoveredEvidence(odu Odu) []relationships.EvidenceFact {
	catalog := RepositoryCatalog(odu.Facts)
	return relationships.DiscoverEvidence(odu.Facts, catalog)
}

// EvidenceSatisfies reports whether ev proves rc: at least one evidence fact
// must carry rc.Relationship as its RelationshipType for every evidence kind
// rc.EvidenceKinds names (rc.EvidenceKinds ⊆ observed evidence kinds on that
// relationship). rc's with no EvidenceKinds filter are never satisfied here —
// Ifá only proves the evidence-narrowed correlations (rc-29..rc-36 as of
// #4394); unfiltered correlations stay golden-gate-owned (design §1d). When
// unsatisfied, detail names the missing evidence kind(s) so a false-green break
// (see coverage_falsegreen_test.go) is diagnosable without re-deriving evidence
// by hand.
func EvidenceSatisfies(rc goldengate.RequiredCorrelation, ev []relationships.EvidenceFact) (bool, string) {
	if len(rc.EvidenceKinds) == 0 {
		return false, fmt.Sprintf("required correlation %s has no evidence_kinds filter; Ifá only proves evidence-narrowed correlations", rc.ID)
	}

	required := map[string]struct{}{}
	for _, kind := range rc.EvidenceKinds {
		required[kind] = struct{}{}
	}

	observed := map[relationships.EvidenceKind]struct{}{}
	var matching int64
	for _, e := range ev {
		if string(e.RelationshipType) != rc.Relationship {
			continue
		}
		observed[e.EvidenceKind] = struct{}{}
		if _, ok := required[string(e.EvidenceKind)]; ok {
			matching++
		}
	}

	var missing []string
	for _, kind := range rc.EvidenceKinds {
		if _, ok := observed[relationships.EvidenceKind(kind)]; !ok {
			missing = append(missing, kind)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf(
			"relationship %s missing evidence kind(s) [%s]; observed kind(s) on that relationship: [%s]",
			rc.Relationship, strings.Join(missing, ", "), strings.Join(observedEvidenceKinds(observed), ", "),
		)
	}

	// Mirror the B-12 gate's existence semantics (goldengate.EvaluateRequired
	// Correlation): at least MinimumCount narrowed edges must exist, so require
	// at least that many matching evidence facts. Otherwise a min-counted
	// correlation (e.g. rc-35, minimum_count 8) would be marked covered by a
	// single matching fact while the golden-corpus gate still fails.
	want := rc.MinimumCount
	if want < 1 {
		want = 1
	}
	if matching < want {
		return false, fmt.Sprintf(
			"relationship %s carries required evidence kind(s) %v but only %d matching evidence fact(s), want >= %d",
			rc.Relationship, rc.EvidenceKinds, matching, want,
		)
	}
	return true, fmt.Sprintf(
		"relationship %s carries required evidence kind(s) %v across %d matching evidence fact(s) (>= %d)",
		rc.Relationship, rc.EvidenceKinds, matching, want,
	)
}

func observedEvidenceKinds(observed map[relationships.EvidenceKind]struct{}) []string {
	kinds := make([]string, 0, len(observed))
	for kind := range observed {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	return kinds
}
