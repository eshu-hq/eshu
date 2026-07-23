// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildContentEntityRecordDependencyFallbackMatchesShapeMint is test (e)
// from the #5357 locked spec: the two mint sites — shape.Materialize's
// per-file mint and buildContentEntityRecord's no-entity_id fallback — MUST
// agree on the same section-keyed identity for the same logical dependency
// row. The fallback only fires when a fact arrives without a collector-minted
// entity_id (version skew, replayed old cassettes, non-git producers); a
// divergence here would silently corrupt identity on exactly that path, which
// is why the spec calls this lockstep mandatory.
func TestBuildContentEntityRecordDependencyFallbackMatchesShapeMint(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repository:r_12345678"
		path    = "package.json"
		name    = "react"
		section = "dependencies"
		line    = 12
	)

	dependencyMetadata := map[string]any{
		"section":         section,
		"config_kind":     "dependency",
		"package_manager": "npm",
		"lang":            "json",
	}

	// The shape.Materialize side: the same row a collector would parse out
	// of package.json and mint an entity_id for at ingest time.
	materialized, err := shape.Materialize(shape.Input{
		RepoID: repoID,
		Files: []shape.File{
			{
				Path: path,
				Body: "{}",
				EntityBuckets: map[string][]shape.Entity{
					"variables": {
						{Name: name, LineNumber: line, Metadata: dependencyMetadata},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("shape.Materialize() error = %v, want nil", err)
	}
	if len(materialized.Entities) != 1 {
		t.Fatalf("len(shape.Materialize().Entities) = %d, want 1", len(materialized.Entities))
	}
	mintedID := materialized.Entities[0].EntityID

	// The projector fallback side: a content_entity fact WITHOUT entity_id
	// (the version-skew / replayed-cassette / non-git-producer case) but
	// carrying entity_metadata with the same section/config_kind/
	// package_manager keys entityMetadataFromPayload passes through.
	fact := facts.Envelope{
		FactID:   "fact-dep-no-entity-id",
		FactKind: "content_entity",
		Payload: map[string]any{
			"content_path":    path,
			"entity_type":     "Variable",
			"entity_name":     name,
			"start_line":      float64(line),
			"entity_metadata": dependencyMetadata,
		},
	}

	record, ok := buildContentEntityRecord(repoID, fact)
	if !ok {
		t.Fatalf("buildContentEntityRecord() ok = false, want true")
	}

	if record.EntityID != mintedID {
		t.Fatalf("buildContentEntityRecord() fallback entity_id = %q, want shape.Materialize's minted id %q (two-site divergence)", record.EntityID, mintedID)
	}

	// Sanity: the agreed id must actually be the section-keyed dependency
	// form, not a coincidental legacy-scheme match.
	wantID := content.CanonicalDependencyEntityID(repoID, path, section, name)
	if record.EntityID != wantID {
		t.Fatalf("agreed entity_id = %q, want CanonicalDependencyEntityID() = %q", record.EntityID, wantID)
	}

	// The record's Metadata field must carry the same metadata the fallback
	// gated on — the spec requires computing entityMetadataFromPayload once
	// and using it for BOTH the mint fallback and the Metadata field.
	if got, want := record.Metadata["section"], section; got != want {
		t.Fatalf("record.Metadata[section] = %#v, want %#v", got, want)
	}
}

// TestBuildContentEntityRecordUsesEntityIDVerbatimWhenPresent is the second
// case of test (e): a fact WITH entity_id must use it verbatim regardless of
// what entity_metadata says — the fallback mint path (and therefore the
// dependency gate) must never run when the collector already minted an id.
func TestBuildContentEntityRecordUsesEntityIDVerbatimWhenPresent(t *testing.T) {
	t.Parallel()

	const collectorMintedID = "content-entity:e_deadbeef0000"

	fact := facts.Envelope{
		FactID:   "fact-dep-with-entity-id",
		FactKind: "content_entity",
		Payload: map[string]any{
			"content_path": "package.json",
			"entity_type":  "Variable",
			"entity_name":  "react",
			"start_line":   float64(12),
			"entity_id":    collectorMintedID,
			"entity_metadata": map[string]any{
				"section":         "dependencies",
				"config_kind":     "dependency",
				"package_manager": "npm",
			},
		},
	}

	record, ok := buildContentEntityRecord("repository:r_12345678", fact)
	if !ok {
		t.Fatalf("buildContentEntityRecord() ok = false, want true")
	}

	if record.EntityID != collectorMintedID {
		t.Fatalf("record.EntityID = %q, want collector-minted id %q used verbatim", record.EntityID, collectorMintedID)
	}
}

// TestBuildContentEntityRecordDependencyFallbackMatchesShapeMintForDiscriminatedFormat
// extends the #5357 two-site lockstep proof to a #5507 format that carries a
// package-manager-specific identity discriminator (maven's classifier/type):
// the projector's no-entity_id fallback and shape.Materialize's per-file mint
// must still agree once the discriminator is folded in, not just for the
// discriminator-less npm/composer case above.
func TestBuildContentEntityRecordDependencyFallbackMatchesShapeMintForDiscriminatedFormat(t *testing.T) {
	t.Parallel()

	const (
		repoID     = "repository:r_12345678"
		path       = "pom.xml"
		name       = "io.netty:netty-tcnative-boringssl-static"
		section    = "dependencies"
		classifier = "linux-x86_64"
		line       = 10
	)

	dependencyMetadata := map[string]any{
		"section":               section,
		"config_kind":           "dependency",
		"package_manager":       "maven",
		"lang":                  "maven",
		"dependency_classifier": classifier,
	}

	materialized, err := shape.Materialize(shape.Input{
		RepoID: repoID,
		Files: []shape.File{
			{
				Path: path,
				Body: "<project></project>",
				EntityBuckets: map[string][]shape.Entity{
					"variables": {
						{Name: name, LineNumber: line, Metadata: dependencyMetadata},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("shape.Materialize() error = %v, want nil", err)
	}
	if len(materialized.Entities) != 1 {
		t.Fatalf("len(shape.Materialize().Entities) = %d, want 1", len(materialized.Entities))
	}
	mintedID := materialized.Entities[0].EntityID

	fact := facts.Envelope{
		FactID:   "fact-dep-maven-no-entity-id",
		FactKind: "content_entity",
		Payload: map[string]any{
			"content_path":    path,
			"entity_type":     "Variable",
			"entity_name":     name,
			"start_line":      float64(line),
			"entity_metadata": dependencyMetadata,
		},
	}

	record, ok := buildContentEntityRecord(repoID, fact)
	if !ok {
		t.Fatalf("buildContentEntityRecord() ok = false, want true")
	}

	if record.EntityID != mintedID {
		t.Fatalf("buildContentEntityRecord() fallback entity_id = %q, want shape.Materialize's minted id %q (two-site divergence on a discriminated format)", record.EntityID, mintedID)
	}

	if legacy := content.CanonicalEntityID(repoID, path, "Variable", name, line); record.EntityID == legacy {
		t.Fatalf("agreed entity_id = %q unexpectedly matched legacy CanonicalEntityID()", record.EntityID)
	}
}
