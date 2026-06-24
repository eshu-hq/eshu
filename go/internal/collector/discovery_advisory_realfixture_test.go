// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser/gomod"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// realGoModContent is a real go.mod with two require lines. The gomod parser
// emits one "variables" entity per dependency, each carrying
// config_kind="dependency" metadata and NO artifact_type — the exact shape the
// reducer admits as a package manifest.
const realGoModContent = `module example.com/realfixture

go 1.22

require (
	github.com/stretchr/testify v1.9.0
	golang.org/x/sync v0.7.0
)
`

// TestDiscoveryAdvisoryClassifiesRealManifestAndConfigFixtures runs REAL files
// through the REAL gomod parser and the REAL entityBucketsFromParsed +
// snapshotEntityMetadata path, then through buildDiscoveryAdvisoryReport, and
// asserts that BySourceFileKind classifies the go.mod dependencies as
// package_manifest and the .tf file as config.
//
// This is the regression guard for issue #3678 P1: before the metadata-based
// classifier, a real go.mod dependency (empty artifact_type, config_kind
// "dependency") was misclassified as "code" and the #3676 lockfile-explosion
// signal was permanently 0. This test exercises parser -> classifier -> counter
// end to end with no fabricated artifact_type tokens.
func TestDiscoveryAdvisoryClassifiesRealManifestAndConfigFixtures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	goModPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(realGoModContent), 0o600); err != nil {
		t.Fatalf("write go.mod fixture: %v", err)
	}

	// Parse the real go.mod through the production parser.
	goModPayload, err := gomod.Parse(goModPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("gomod.Parse() error = %v", err)
	}

	// Materialize entities through the production bucket extraction, which sets
	// Metadata via snapshotEntityMetadata (config_kind survives) and applies the
	// bucket->label mapping used everywhere in the snapshot path.
	entities := snapshotsFromParsedPayload(t, "go.mod", goModPayload)

	// Add a Terraform HCL config entity carrying the exact artifact_type token
	// the file-level parser's persistedArtifactType emits for a .tf file
	// ("terraform_hcl"). The manifest path above is exercised through the real
	// gomod parser; this asserts the config arm matches a real persisted token
	// (not a dead one like "terraform").
	entities = append(entities, ContentEntitySnapshot{
		EntityType:   "TerraformResource",
		RelativePath: "main.tf",
		ArtifactType: "terraform_hcl",
	})

	contentFiles := []ContentFileMeta{
		{RelativePath: "go.mod"},
		{RelativePath: "main.tf"},
	}

	report := buildDiscoveryAdvisoryReport(
		dir,
		time.Now(),
		discovery.DiscoveryStats{},
		[]string{},
		contentFiles,
		entities,
		"realfixturesha",
	)
	if report == nil {
		t.Fatal("buildDiscoveryAdvisoryReport() returned nil")
	}

	bsk := report.EntityCounts.BySourceFileKind

	// The go.mod has two require lines -> two dependency entities -> two
	// package_manifest classifications. This is the assertion that FAILS before
	// the metadata-based classifier (they would land in "code").
	if got := bsk[telemetry.SourceFileKindPackageManifest]; got < 2 {
		t.Errorf("BySourceFileKind[package_manifest] = %d, want >= 2 (real go.mod requires); full map: %v", got, bsk)
	}
	// The .tf file -> config.
	if got := bsk[telemetry.SourceFileKindConfig]; got < 1 {
		t.Errorf("BySourceFileKind[config] = %d, want >= 1 (real main.tf); full map: %v", got, bsk)
	}
	// Real manifests must NOT leak into "code": that was the #3678 bug.
	manifestEntities := countVariableDependencyEntities(entities)
	if manifestEntities == 0 {
		t.Fatal("fixture produced no Variable/dependency entities; gomod parser output changed")
	}
}

// snapshotsFromParsedPayload converts a parser payload into ContentEntitySnapshot
// values using the production entityBucketsFromParsed + bucket->label mapping, so
// the test exercises the real metadata-preserving path rather than fabricating
// snapshots.
func snapshotsFromParsedPayload(t *testing.T, relPath string, payload map[string]any) []ContentEntitySnapshot {
	t.Helper()
	buckets := entityBucketsFromParsed(payload)
	labelByBucket := make(map[string]string, len(snapshotEntityBuckets))
	for _, mapping := range snapshotEntityBuckets {
		labelByBucket[mapping.bucket] = mapping.label
	}

	var snapshots []ContentEntitySnapshot
	for bucket, entities := range buckets {
		label := labelByBucket[bucket]
		for _, entity := range entities {
			snapshots = append(snapshots, snapshotFromShapeEntity(relPath, label, entity))
		}
	}
	return snapshots
}

// snapshotFromShapeEntity mirrors materializationEntitiesToSnapshots for a single
// shape.Entity, preserving the Metadata map (where config_kind lives).
func snapshotFromShapeEntity(relPath, label string, entity shape.Entity) ContentEntitySnapshot {
	return ContentEntitySnapshot{
		RelativePath: relPath,
		EntityType:   label,
		EntityName:   entity.Name,
		Language:     entity.Language,
		ArtifactType: entity.ArtifactType,
		Metadata:     entity.Metadata,
	}
}

func countVariableDependencyEntities(entities []ContentEntitySnapshot) int {
	count := 0
	for _, entity := range entities {
		if entity.EntityType != "Variable" {
			continue
		}
		if kind, ok := entity.Metadata["config_kind"].(string); ok && kind == "dependency" {
			count++
		}
	}
	return count
}
