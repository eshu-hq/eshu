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
// The bar is equality, in both directions. Containment alone would let the two
// paths drift apart as long as one stayed a superset, and the drift would be
// invisible: both facts are processed and their evidence unions, so a
// disagreement widens the graph rather than failing anything.
//
// Equality is reachable because the raw gather now walks the legacy `bases:`
// key too. Before that it saw only `resources` and `components`, so a target
// written under `bases:` produced evidence on one path and not the other.
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

	// The catalog has to know the repositories every entry names, including the
	// ones under the legacy `bases:` key. A value that matches nothing produces
	// no evidence on either path, so it would satisfy the equality check below
	// without either path ever having looked at it.
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

	// Two empty sets are equal, so a fixture that stopped matching the catalog
	// would turn this into a test that proves nothing while staying green.
	if len(rawKeys) == 0 {
		t.Fatal("the raw path produced no evidence; the fixture no longer matches the " +
			"catalog and the comparisons below would pass vacuously")
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

	if len(extra) != 0 {
		t.Errorf("the structured path produced %#v and the raw path did not; the two "+
			"reads of one file must agree, or the graph carries the union of both "+
			"answers with nothing reporting the disagreement", extra)
	}

	// The fixture has to exercise the legacy bases: key, or equality holds for
	// the uninteresting reason that neither path ever sees one.
	if _, ok := rawKeys["KUSTOMIZE_RESOURCE_REFERENCE|github.com/acme/platform-base//k8s?ref=v2.0.0"]; !ok {
		t.Error("neither path produced evidence for the target under the legacy bases: key; " +
			"the equality above then proves nothing about that key")
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
