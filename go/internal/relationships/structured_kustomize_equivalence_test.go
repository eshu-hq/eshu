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
// The bar is containment, and equality is NOT available — which is worth
// stating plainly, because an earlier version of this test asserted equality
// and got it by comparing against the wrong function.
//
// The production content-fact path is discoverKustomizeEvidence, and it does
// two things: the per-document extraction the structured path mirrors, AND a
// catch-all that runs a regex over the whole file and offers every `key: value`
// scalar to the catalog matcher. The structured path has no equivalent, and
// should not — the catch-all is a property of reading raw text, not of reading
// the parser's typed lists. Comparing against discoverKustomizeDocumentEvidence
// hid that entirely (#5609 review, codex).
//
// So what has to hold is: everything the structured read produces, the raw read
// also produces. That keeps the file fact from contributing evidence the
// content fact would not, which is the direction that could surprise someone —
// the two facts' evidence unions, so the graph already carries the raw
// superset.
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
	// The production function, not the per-document helper inside it.
	raw := discoverKustomizeEvidence(
		"repo-infra", "overlays/kustomization.yaml", equivalenceKustomization, "sha-1",
		rawMatcher, rawSeen,
	)

	structuredKeys := evidenceTargetKinds(structured)
	rawKeys := evidenceTargetKinds(raw)

	// Two empty sets are equal, so a fixture that stopped matching the catalog
	// would turn this into a test that proves nothing while staying green.
	if len(rawKeys) == 0 {
		t.Fatal("the raw path produced no evidence; the fixture no longer matches the " +
			"catalog and the comparisons below would pass vacuously")
	}

	// Only structured-subset-of-raw is asserted. The reverse does not hold and
	// must not be asserted: the catch-all emits values the structured read has
	// no way to produce, and it tags them KUSTOMIZE_RESOURCE_REFERENCE whatever
	// key they came from — `name: payments-service` under `images:` becomes a
	// resource reference on the raw path and an image reference on the typed
	// one. That is the raw path's regex being coarse, not the typed read
	// dropping anything.
	var extra []string
	for key := range structuredKeys {
		if _, ok := rawKeys[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)

	if len(extra) != 0 {
		t.Errorf("the structured path produced %#v and the raw path did not; the file "+
			"fact must not contribute evidence the content fact would not, since the "+
			"two union into one graph", extra)
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
