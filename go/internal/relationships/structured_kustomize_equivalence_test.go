// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	yamlparser "github.com/eshu-hq/eshu/go/internal/parser/yaml"
)

// One kustomization carrying every entry shape that behaves differently:
// an unambiguous same-repo path, a relative sibling (the calibration corpus's
// 0.90 positive), a scheme-less remote, a file reference, a versioned local
// directory, a component, and both keys of the legacy/modern base split.
const equivalenceKustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ./base
  - ../payments-service/base
  - github.com/acme/deployable-source//k8s?ref=v1.4.0
  - deployment.yaml
  - v1.2/base
components:
  - ../components/logging
bases:
  - github.com/acme/platform-base//k8s?ref=v2.0.0
  - ../common
helmCharts:
  - name: redis
    repo: https://charts.example.com
    releaseName: cache
images:
  - name: payments-service
    newName: registry.example.com/payments
`

// Reading the parser's bucket instead of re-parsing the file must not silently
// change which strings reach the catalog matcher. This is the "config-vs-raw
// discrepancy" #5609 asks to resolve, asserted rather than argued.
//
// The bar is containment, not equality: every value the raw path produced must
// still be produced. The structured path is allowed to find MORE, and it does —
// the raw gather walks `resources` and `components` only, so it never saw the
// legacy `bases:` key. Those extra values are asserted explicitly below so a
// future change cannot quietly widen the set further without updating this
// test.
func TestStructuredKustomizeCoversEveryRawValue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "kustomization.yaml")
	if err := os.WriteFile(path, []byte(equivalenceKustomization), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	payload, err := yamlparser.Parse(path, false, yamlparser.Options{})
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	// The catalog has to know the repositories the legacy `bases:` entries
	// name, or the structured path's extra values match nothing and the gain
	// is invisible — the test would pass while proving less than it claims.
	matcherFor := func() (*catalogMatcher, map[evidenceKey]struct{}) {
		return newCatalogMatcher([]CatalogEntry{
			{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
			{RepoID: "repo-platform", Aliases: []string{"platform-base"}},
			{RepoID: "repo-common", Aliases: []string{"common"}},
		}), make(map[evidenceKey]struct{})
	}

	structuredMatcher, structuredSeen := matcherFor()
	structured := discoverStructuredKustomizeEvidence(
		"repo-infra", "overlays/kustomization.yaml", "sha-1",
		payload, structuredMatcher, structuredSeen,
	)

	rawMatcher, rawSeen := matcherFor()
	documents := parseYAMLDocuments(equivalenceKustomization)
	if len(documents) != 1 {
		t.Fatalf("parseYAMLDocuments returned %d documents, want 1", len(documents))
	}
	raw := discoverKustomizeDocumentEvidence(
		"repo-infra", "overlays/kustomization.yaml", documents[0],
		rawMatcher, rawSeen, "sha-1",
	)

	structuredKeys := evidenceTargetKinds(structured)
	rawKeys := evidenceTargetKinds(raw)

	// Containment is trivially true against an empty set, so a fixture that
	// stopped matching the catalog would turn this into a test that proves
	// nothing while staying green.
	if len(rawKeys) == 0 {
		t.Fatal("the raw path produced no evidence; the fixture no longer matches the " +
			"catalog and the containment check below would pass vacuously")
	}

	for key := range rawKeys {
		if _, ok := structuredKeys[key]; !ok {
			t.Errorf("the raw path produced %s but the structured path did not; "+
				"reading the parser bucket must not drop evidence the re-parse found", key)
		}
	}

	var extra []string
	for key := range structuredKeys {
		if _, ok := rawKeys[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)

	// Both come from the legacy `bases:` key, which the raw gather never walks.
	want := []string{
		"KUSTOMIZE_RESOURCE_REFERENCE|../common",
		"KUSTOMIZE_RESOURCE_REFERENCE|github.com/acme/platform-base//k8s?ref=v2.0.0",
	}
	if !equalStringSlices(extra, want) {
		t.Errorf("structured-only evidence = %#v, want %#v: the only values the "+
			"structured path may add are the legacy bases: entries the raw gather "+
			"cannot see", extra, want)
	}
}

// evidenceTargetKinds keys evidence by kind and matched value, which is what
// identifies a finding for this comparison — confidence and rationale are
// carried by the shared registry and are equal on both paths by construction.
func evidenceTargetKinds(facts []EvidenceFact) map[string]struct{} {
	keys := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		value, _ := fact.Details["matched_value"].(string)
		if value == "" {
			value = fact.TargetRepoID
		}
		keys[string(fact.EvidenceKind)+"|"+value] = struct{}{}
	}
	return keys
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
