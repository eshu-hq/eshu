// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// packageRegistryCassetteArtifactScopeID is the cassette scope this test locks:
// the "go:github.com/acme/lib-common" scope, which carries the package,
// package_version, package_dependency, source_hint, and (#5458)
// package_artifact and registry_event facts this projector wave/consumer
// promotion covers. The
// sibling "npm:synthetic-vulnerable-npm" scope carries a
// package_registry.package_version-kind fact shaped for a different, older
// consumer (name/version/dist_integrity fields, no package_id/version_id) that
// already quarantines through today's typed decode seam independent of this
// change; that is a pre-existing gap outside #5458's package_artifact scope,
// so this test scopes to the acme scope rather than asserting the whole
// cassette decodes cleanly.
const packageRegistryCassetteArtifactScopeID = "package_registry:go:github.com/acme/lib-common"

// TestPackageRegistryCassetteDecodesCleanlyThroughSeam is the durable,
// no-Docker guard for the B-7 golden-corpus cassette's acme scope
// (testdata/cassettes/packageregistry/supply-chain-demo.json): every fact it
// carries must decode through extractPackageRegistryRows without quarantine,
// and the #5458 package_artifact and registry_event facts this test's
// fixture adds must materialize their PackageRegistryArtifact/
// PackageRegistryEvent rows with the per-artifact hash digests and event
// identity intact. A cassette payload that drifts from the collector emitter's
// shape (go/internal/collector/packageregistry/*.NewXEnvelope) would otherwise
// only surface as a red golden-corpus gate, which runs Docker and requires a
// live run to diagnose. This test closes that gap at `go test
// ./internal/projector`, mirroring TestOCIRegistryCassetteDecodesCleanlyThroughSeam.
func TestPackageRegistryCassetteDecodesCleanlyThroughSeam(t *testing.T) {
	t.Parallel()

	envelopes := loadPackageRegistryCassetteEnvelopes(t, packageRegistryCassetteArtifactScopeID)
	if len(envelopes) == 0 {
		t.Fatal("cassette carried no package_registry facts for the acme scope; the golden-corpus gate would project nothing")
	}

	mat := &CanonicalMaterialization{}
	quarantined := extractPackageRegistryRows(mat, envelopes)

	if len(quarantined) != 0 {
		for _, q := range quarantined {
			t.Errorf("cassette fact %s (%s) quarantined as input_invalid on field %q; the cassette payload has drifted from the current collector emitter's shape — reconcile testdata/cassettes/packageregistry/supply-chain-demo.json to go/internal/collector/packageregistry/*.NewXEnvelope", q.factID, q.factKind, q.field)
		}
		t.FailNow()
	}

	if len(mat.PackageRegistryPackages) == 0 {
		t.Error("cassette package fact did not materialize a PackageRegistryPackage row")
	}
	if len(mat.PackageRegistryVersions) == 0 {
		t.Error("cassette package_version fact did not materialize a PackageRegistryVersion row")
	}
	if len(mat.PackageRegistryArtifacts) == 0 {
		t.Fatal("cassette package_artifact fact did not materialize a PackageRegistryArtifact row; the golden-corpus gate's PackageArtifact node_count and rc-168 HAS_ARTIFACT checks would fail")
	}

	artifact := mat.PackageRegistryArtifacts[0]
	if artifact.ArtifactKey == "" {
		t.Error("cassette package_artifact row carries an empty ArtifactKey")
	}
	if len(artifact.Hashes) == 0 {
		t.Error("cassette package_artifact row carries no Hashes; the #5458 per-artifact digest binding did not survive decode")
	}
	if _, ok := artifact.Hashes["sha256"]; !ok {
		t.Errorf("cassette package_artifact row Hashes = %#v, want a sha256 entry", artifact.Hashes)
	}

	if len(mat.PackageRegistryEvents) == 0 {
		t.Fatal("cassette registry_event fact did not materialize a PackageRegistryEvent row; the golden-corpus gate's RegistryEvent node_count and rc-169 HAS_REGISTRY_EVENT checks would fail")
	}
	event := mat.PackageRegistryEvents[0]
	if event.EventKey == "" {
		t.Error("cassette registry_event row carries an empty EventKey")
	}
	if event.EventType != "yank" {
		t.Errorf("cassette registry_event row EventType = %q, want %q", event.EventType, "yank")
	}
	if event.VersionID == "" {
		t.Error("cassette registry_event row carries an empty VersionID; the deferred HAS_REGISTRY_EVENT edge MATCH would find no PackageVersion to attach to")
	}
}

// loadPackageRegistryCassetteEnvelopes reads the real checked-in
// package_registry cassette and converts each fact recorded under scopeID
// into a facts.Envelope carrying the fact kind, schema version, and payload
// extractPackageRegistryRows consumes. It intentionally reads the same file
// the golden-corpus gate replays, so a drift in that file is caught here.
func loadPackageRegistryCassetteEnvelopes(t *testing.T, scopeID string) []facts.Envelope {
	t.Helper()

	// This file lives at <repoRoot>/go/internal/projector/; the cassette lives
	// at <repoRoot>/testdata/cassettes/packageregistry/supply-chain-demo.json.
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	cassettePath := filepath.Join(wd, "..", "..", "..", "testdata", "cassettes", "packageregistry", "supply-chain-demo.json")

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read package_registry cassette %s: %v", cassettePath, err)
	}

	var cassette struct {
		Scopes []struct {
			GenerationID string `json:"generation_id"`
			ScopeID      string `json:"scope_id"`
			Facts        []struct {
				FactKind      string         `json:"fact_kind"`
				SchemaVersion string         `json:"schema_version"`
				StableFactKey string         `json:"stable_fact_key"`
				Payload       map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &cassette); err != nil {
		t.Fatalf("unmarshal package_registry cassette: %v", err)
	}

	var envelopes []facts.Envelope
	for _, scope := range cassette.Scopes {
		if scope.ScopeID != scopeID {
			continue
		}
		for i, fact := range scope.Facts {
			envelopes = append(envelopes, facts.Envelope{
				FactID:        packageRegistryCassetteFactID(fact.FactKind, i),
				ScopeID:       scope.ScopeID,
				GenerationID:  scope.GenerationID,
				FactKind:      fact.FactKind,
				SchemaVersion: fact.SchemaVersion,
				StableFactKey: fact.StableFactKey,
				Payload:       fact.Payload,
			})
		}
	}
	return envelopes
}

// packageRegistryCassetteFactID synthesizes a stable, unique fact id for a
// cassette fact so a quarantine message can name the offending fact
// deterministically.
func packageRegistryCassetteFactID(factKind string, index int) string {
	return "cassette:" + factKind + ":" + string(rune('0'+index))
}
