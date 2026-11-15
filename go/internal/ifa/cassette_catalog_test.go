// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// vulnOduScopes lists, for each cataloged vulnerability-collector Odù, the
// recorded scopes it selects from the cassette.
var vulnOduScopes = map[string][]string{
	VulnCollectorOduName:              {vulnWindow1Scope, vulnKEVScope, vulnEPSSScope},
	VulnCollectorWildcardRangeOduName: {vulnWindow1Scope},
	VulnCollectorRejectedOduName:      {vulnWindow1Scope},
	VulnCollectorNewestCopyOduName:    {vulnWindow1Scope, vulnWindow2Scope},
	VulnCollectorNoBaselineOduName:    {vulnWindow1Scope},
}

// plantedKinds are the three SBOM-side kinds every vulnerability-collector Odù
// plants beside the recorded facts: the NVD product path needs all three to
// reach an owned image.
var plantedKinds = []string{
	chain.SBOMComponentFactKind,
	reducercontract.SBOMAttestationAttachmentFactKind,
	reducercontract.ContainerImageIdentityFactKind,
}

func registryByKind() map[string]facts.FactKindRegistryEntry {
	entries := facts.FactKindRegistry()
	byKind := make(map[string]facts.FactKindRegistryEntry, len(entries))
	for _, entry := range entries {
		byKind[entry.Kind] = entry
	}
	return byKind
}

func countKind(odu familyodu.Odu, kind string) int {
	n := 0
	for _, fact := range odu.Facts {
		if fact.FactKind == kind {
			n++
		}
	}
	return n
}

// recordedScopeIDs returns the sorted distinct scopes of the recorded
// vulnerability facts an Odù carries; the planted SBOM facts sit in scopes of
// their own.
func recordedScopeIDs(odu familyodu.Odu) []string {
	seen := map[string]bool{}
	for _, fact := range odu.Facts {
		if strings.HasPrefix(fact.FactKind, "vulnerability.") {
			seen[fact.ScopeID] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func TestCatalogResolvesCassetteBackedOdus(t *testing.T) {
	t.Parallel()

	catalog, err := CatalogByNameAt(repoRootDir(t))
	if err != nil {
		t.Fatalf("CatalogByNameAt() error = %v, want nil", err)
	}
	byKind := registryByKind()
	for name, scopes := range vulnOduScopes {
		odu, ok := catalog[name]
		if !ok {
			t.Errorf("CatalogByNameAt() is missing %q", name)
			continue
		}
		want := slices.Clone(scopes)
		slices.Sort(want)
		if got := recordedScopeIDs(odu); !slices.Equal(got, want) {
			t.Errorf("%s recorded scopes = %v, want %v", name, got, want)
		}
		for _, kind := range plantedKinds {
			if got := countKind(odu, kind); got != 1 {
				t.Errorf("%s carries %d planted %q facts, want exactly one", name, got, kind)
			}
		}
		for _, fact := range odu.Facts {
			if strings.HasPrefix(fact.FactKind, "vulnerability.") && (fact.FactID == "" || fact.StableFactKey == "") {
				t.Errorf("%s: recorded %s fact has no fact id or stable key; it was not built the way a replay builds it", name, fact.FactKind)
			}
		}
		if err := ValidateOduPayloads(odu, byKind); err != nil {
			t.Errorf("ValidateOduPayloads(%s) = %v, want nil: the recorded and planted payloads must satisfy their schemas", name, err)
		}
	}

	compiled := CatalogByName()
	for name := range compiled {
		if _, ok := catalog[name]; !ok {
			t.Errorf("compiled Odù %q is missing from the cassette-backed catalog", name)
		}
	}
	if got, want := len(catalog), len(compiled)+len(vulnOduScopes); got != want {
		t.Errorf("len(catalog) = %d, want %d compiled plus %d cassette-backed", got, want, len(vulnOduScopes))
	}
}

func TestCatalogAtIsSortedAndDescribesEveryOdu(t *testing.T) {
	t.Parallel()

	entries, err := CatalogAt(repoRootDir(t))
	if err != nil {
		t.Fatalf("CatalogAt() error = %v, want nil", err)
	}
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Odu.Name < entries[j].Odu.Name }) {
		t.Error("CatalogAt() is not sorted by Odù name")
	}
	for _, entry := range entries {
		if _, ok := vulnOduScopes[entry.Odu.Name]; ok && strings.TrimSpace(entry.Detail) == "" {
			t.Errorf("%s has no detail for coverage reporting", entry.Odu.Name)
		}
	}
}

// writeCassette marshals scopes into a v1 cassette at the vulnerability
// collector's cassette path under root.
func writeCassette(t *testing.T, root string, scopes []cassette.Scope) {
	t.Helper()
	raw, err := json.Marshal(cassette.File{Collector: "vulnerability_intelligence", SchemaVersion: "1", Scopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(vulnCollectorCassetteRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func cassetteScope(scopeID string, withFact bool) cassette.Scope {
	scope := cassette.Scope{
		ScopeID: scopeID, SourceSystem: "vulnerability_intelligence", ScopeKind: "vulnerability_intelligence",
		CollectorKind: "vulnerability_intelligence", GenerationID: "generation", ObservedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if withFact {
		scope.Facts = []cassette.Fact{{
			FactKind: "vulnerability.cve", StableFactKey: "key", SchemaVersion: "1.0.0",
			Payload: map[string]any{"cve_id": "CVE-2026-90001"},
		}}
	}
	return scope
}

// TestCatalogFailsClosedWhenCassetteMissing: an Odù whose recording is gone
// must not resolve as an empty one, because every assertion on an empty Odù
// passes vacuously.
func TestCatalogFailsClosedWhenCassetteMissing(t *testing.T) {
	t.Parallel()

	_, err := CatalogByNameAt(t.TempDir())
	if err == nil {
		t.Fatal("CatalogByNameAt(empty repo root) error = nil, want a missing cassette error")
	}
	if !strings.Contains(err.Error(), "vuln-collector-nvd.json") {
		t.Errorf("error = %v, want it to name the cassette file", err)
	}
}

func TestCatalogFailsClosedWhenCassetteIsHollow(t *testing.T) {
	t.Parallel()

	all := []string{vulnWindow1Scope, vulnWindow2Scope, vulnWindowXScope, vulnKEVScope, vulnEPSSScope}
	for _, tc := range []struct {
		name    string
		scopes  []cassette.Scope
		mention string
	}{
		{"no scopes at all", nil, "vuln-collector-nvd.json"},
		{"a selected scope is absent", []cassette.Scope{cassetteScope(vulnKEVScope, true), cassetteScope(vulnEPSSScope, true)}, vulnWindow1Scope},
		{"a selected scope holds no facts", func() []cassette.Scope {
			out := make([]cassette.Scope, 0, len(all))
			for _, id := range all {
				out = append(out, cassetteScope(id, id != vulnWindow1Scope))
			}
			return out
		}(), vulnWindow1Scope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeCassette(t, root, tc.scopes)
			_, err := CatalogByNameAt(root)
			if err == nil {
				t.Fatal("CatalogByNameAt() error = nil, want a fail-closed error")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error = %v, want it to mention %q", err, tc.mention)
			}
		})
	}
}

// globsCover reports whether a registry trigger or filter glob list selects
// path: an exact entry, or a directory entry ending in "/**".
func globsCover(globs []string, path string) bool {
	for _, glob := range globs {
		if glob == path {
			return true
		}
		if prefix, ok := strings.CutSuffix(glob, "**"); ok && strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// TestCassettePathsAreContractLayerTriggers: a change to the recording alone
// must schedule the ifa-contract-layer gate, in the registry and in the
// workflow's path filter. Neither lists the vulnerability collector's cassette
// directory otherwise, so a cassette-only change would skip the gate that reads
// it. A mismatch between the two lists once stalled required-gates-complete.
func TestCassettePathsAreContractLayerTriggers(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	registry, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load() error = %v", err)
	}
	var triggers []string
	for _, gate := range registry.Gates {
		if gate.ID == "ifa-contract-layer" {
			triggers = gate.Triggers
		}
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "static-contract-filters.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var filters map[string][]string
	if err := yaml.Unmarshal(raw, &filters); err != nil {
		t.Fatalf("parse static-contract-filters.yml: %v", err)
	}

	for _, list := range []struct {
		label string
		globs []string
	}{
		{"the registry triggers of ifa-contract-layer", triggers},
		{"the ifa filter group of static-contract-filters.yml", filters["ifa"]},
	} {
		if len(list.globs) == 0 {
			t.Fatalf("%s is empty; the lookup found nothing", list.label)
		}
		if !globsCover(list.globs, vulnCollectorCassetteRelPath) {
			t.Errorf("%s does not select %s", list.label, vulnCollectorCassetteRelPath)
		}
	}
}
