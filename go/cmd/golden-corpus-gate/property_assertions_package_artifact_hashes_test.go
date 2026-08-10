// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// packageArtifactHashesSnapshot mirrors the rn-package-artifact-hashes entry
// committed in testdata/golden/e2e-20repo-snapshot.json (#5820 P2 review
// finding): the exact digest value pinned for the package_registry
// supply-chain-demo cassette's github.com/acme/lib-common@1.0.0
// package_artifact fact. rc-168 (required_correlations) already asserts the
// PackageVersion-[:HAS_ARTIFACT]->PackageArtifact edge exists, but a live
// projection that creates the node and edge while omitting or corrupting
// `hashes` would still satisfy rc-168 -- this is the value-level counterpart
// that a #5458 feature whose entire purpose is preserving per-artifact
// digests needs.
func packageArtifactHashesSnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: "1",
		Graph: GraphSnapshot{
			RequiredNodes: []RequiredNode{{
				ID:                     "rn-package-artifact-hashes",
				Label:                  "PackageArtifact",
				MinimumCount:           1,
				RequiredNodeProperties: []string{"hashes"},
				AllowedNodePropertyValues: map[string][]string{
					"hashes": {
						"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
					},
				},
			}},
		},
	}
}

// TestCheckGraphPackageArtifactHashesCorrectValuePasses is the non-vacuity
// positive proof for rn-package-artifact-hashes: the PackageArtifact node
// exists and its hashes property -- already "|"-joined the way
// ListNodeProperty/boltPropertyString join a live Bolt list property -- matches
// the pinned exact digest value.
func TestCheckGraphPackageArtifactHashesCorrectValuePasses(t *testing.T) {
	c := fakeCounter{
		nodes: map[string]int64{"PackageArtifact": 1},
		nodeProp: map[string][]string{
			"PackageArtifact|hashes": {
				"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
			},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, packageArtifactHashesSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass when hashes carries the exact pinned digest value; findings: %+v", r.Findings)
	}
}

// TestCheckGraphPackageArtifactHashesMissingFails proves the assertion is
// non-vacuous the way the #5820 P2 finding demanded: a live projection that
// creates the PackageArtifact node (and, via rc-168, the HAS_ARTIFACT edge)
// but OMITS hashes -- the exact defect class rc-168 alone could not catch --
// must fail the gate.
func TestCheckGraphPackageArtifactHashesMissingFails(t *testing.T) {
	c := fakeCounter{
		nodes:    map[string]int64{"PackageArtifact": 1},
		nodeProp: map[string][]string{"PackageArtifact|hashes": {""}},
	}
	var r Report
	if err := checkGraph(context.Background(), c, packageArtifactHashesSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a PackageArtifact node missing hashes must fail the gate")
	}
}

// TestCheckGraphPackageArtifactHashesWrongDigestFails proves the assertion
// checks the VALUE, not just presence: a writer that writes hashes but with a
// corrupted or incomplete digest must fail even though the property is
// non-empty.
func TestCheckGraphPackageArtifactHashesWrongDigestFails(t *testing.T) {
	c := fakeCounter{
		nodes: map[string]int64{"PackageArtifact": 1},
		nodeProp: map[string][]string{
			"PackageArtifact|hashes": {"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, packageArtifactHashesSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a PackageArtifact node carrying a corrupted/incomplete hashes value must fail the gate")
	}
}

// packageArtifactHashesCassetteStableKey identifies the package_artifact
// fact packageArtifactHashesFromCassette reads: the lib-common module the
// pinned rn-package-artifact-hashes snapshot entry describes.
const packageArtifactHashesCassetteStableKey = "package_registry:go:github.com/acme/lib-common:artifact:lib-common-1.0.0.tar.gz"

// packageArtifactHashSegmentEscape mirrors packageRegistryEscapeHashSegment
// (go/internal/storage/cypher/package_registry_artifact_writer.go)
// byte-for-byte: it backslash-escapes every literal '\' and ':' in raw so the
// segment can sit on either side of the ':' separator without being mistaken
// for it. Duplicated rather than imported because packageRegistryEscapeHashSegment
// is unexported; a segment containing neither character is returned
// unchanged, matching the production function's fast path for the common
// case of hex digests and conventional algorithm names.
func packageArtifactHashSegmentEscape(raw string) string {
	if !strings.ContainsAny(raw, `\:`) {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw) + 4)
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' || raw[i] == ':' {
			b.WriteByte('\\')
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

// packageArtifactHashesFromCassette derives the expected PackageArtifact
// hashes property value straight off the recorded package_registry cassette,
// joining its "hashes" map as sorted "algorithm:digest" pairs with "|" —
// mirroring packageRegistryHashPairs
// (go/internal/storage/cypher/package_registry_artifact_writer.go), including
// its packageRegistryEscapeHashSegment escaping and strings.TrimSpace on the
// digest, and boltPropertyString's list join (graph.go). Applying the same
// escape/trim here (rather than a bare "algorithm:digest" join) matters for a
// future digest value: without it, a digest containing ":" or surrounding
// whitespace would derive a different string here than the writer actually
// produces, false-RED-ing this test against a correct graph. Deriving it here
// instead of hand-copying the joined string is the point: a hashes value
// edited in the cassette without updating the pinned snapshot literal now
// fails this test instead of both sides silently drifting in lockstep, which
// is exactly what let the #6011 hand-edit slip through five separate copies
// of the same value.
func packageArtifactHashesFromCassette(t *testing.T) string {
	t.Helper()

	path := filepath.Join("..", "..", "..", "testdata", "cassettes", "packageregistry", "supply-chain-demo.json")
	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("cassette.LoadFile(%s): %v", path, err)
	}

	for _, scope := range file.Scopes {
		for _, fact := range scope.Facts {
			if fact.FactKind != "package_registry.package_artifact" ||
				fact.StableFactKey != packageArtifactHashesCassetteStableKey {
				continue
			}
			hashes, ok := fact.Payload["hashes"].(map[string]any)
			if !ok {
				t.Fatalf("%s: package_artifact hashes is %T, want map[string]any",
					packageArtifactHashesCassetteStableKey, fact.Payload["hashes"])
			}
			pairs := make([]string, 0, len(hashes))
			for algorithm, digest := range hashes {
				digestStr, ok := digest.(string)
				if !ok {
					t.Fatalf("%s: hashes[%q] is %T, want string",
						packageArtifactHashesCassetteStableKey, algorithm, digest)
				}
				pairs = append(pairs, packageArtifactHashSegmentEscape(algorithm)+":"+
					packageArtifactHashSegmentEscape(strings.TrimSpace(digestStr)))
			}
			sort.Strings(pairs)
			return strings.Join(pairs, "|")
		}
	}
	t.Fatalf("cassette %s has no package_registry.package_artifact fact with stable_fact_key %q",
		path, packageArtifactHashesCassetteStableKey)
	return ""
}

// TestLoadSnapshotPackageArtifactHashesEntryMatchesCassette proves the
// COMMITTED rn-package-artifact-hashes entry in
// testdata/golden/e2e-20repo-snapshot.json parses with the expected
// label/property shape and pins the SAME value the package_registry cassette
// actually carries, catching a typo in either the committed snapshot JSON or
// the cassette that the hermetic packageArtifactHashesSnapshot()-based tests
// above cannot catch (they hardcode their own copy of the expected shape,
// and neither side is the cassette).
func TestLoadSnapshotPackageArtifactHashesEntryMatchesCassette(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	var found *RequiredNode
	for i := range snap.Graph.RequiredNodes {
		if snap.Graph.RequiredNodes[i].ID == "rn-package-artifact-hashes" {
			found = &snap.Graph.RequiredNodes[i]
			break
		}
	}
	if found == nil {
		t.Fatal("testdata/golden/e2e-20repo-snapshot.json required_nodes is missing rn-package-artifact-hashes")
	}
	if found.Label != "PackageArtifact" {
		t.Fatalf("Label = %q, want %q", found.Label, "PackageArtifact")
	}
	if len(found.RequiredNodeProperties) != 1 || found.RequiredNodeProperties[0] != "hashes" {
		t.Fatalf("RequiredNodeProperties = %v, want [hashes]", found.RequiredNodeProperties)
	}

	want := packageArtifactHashesFromCassette(t)
	allowed := found.AllowedNodePropertyValues["hashes"]
	if len(allowed) != 1 || allowed[0] != want {
		t.Fatalf("AllowedNodePropertyValues[hashes] = %v, want [%s]", allowed, want)
	}
}
