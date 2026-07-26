// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// This file holds the package_registry.registry_event-specific canonical
// writer tests, in its own file (rather than folded into
// package_registry_canonical_writer_test.go, already at the package's
// 500-line-per-file convention boundary) mirroring the projector-side split
// in package_registry_canonical_event_test.go. packageRegistryPhaseGroupIndex
// is defined in package_registry_canonical_writer_identity_test.go and reused
// here since both files are part of package cypher.

func TestCanonicalNodeWriterBuildsPackageRegistryEventStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryEvents: []projector.PackageRegistryEventRow{{
			UID:                 "package-registry-event-1",
			PackageID:           "package://npm/registry.npmjs.org/@scope/pkg",
			VersionID:           "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			Version:             "1.2.3",
			Ecosystem:           "npm",
			Registry:            "https://registry.npmjs.org",
			EventKey:            "serial:9988",
			EventType:           "yank",
			ArtifactKey:         "pkg-1.2.3.tgz",
			Actor:               "registry-admin",
			Message:             "yanked for CVE-2026-1234",
			SourceFactID:        "package-registry-event-1",
			StableFactKey:       "package-registry-event-1",
			SourceSystem:        "package_registry",
			SourceRecordID:      "package://npm/registry.npmjs.org/@scope/pkg@1.2.3#serial:9988",
			SourceConfidence:    facts.SourceConfidenceReported,
			CollectorKind:       "package_registry",
			CorrelationAnchors:  []string{"package://npm/registry.npmjs.org/@scope/pkg@1.2.3"},
			CollectorInstanceID: "package-registry-collector-1",
		}},
	}

	statements := writer.buildPackageRegistryEventStatements(mat)
	if got, want := len(statements), 1; got != want {
		t.Fatalf("buildPackageRegistryEventStatements() count = %d, want %d", got, want)
	}
	event := statements[0]
	if !strings.Contains(event.Cypher, "MERGE (e:RegistryEvent:PackageRegistryRegistryEvent {uid: row.uid})") {
		t.Fatalf("event Cypher = %q, want RegistryEvent uid merge", event.Cypher)
	}
	if strings.Contains(event.Cypher, "MATCH (") {
		t.Fatalf("event NODE Cypher = %q, must not MATCH PackageVersion (deferred to edge phase)", event.Cypher)
	}
	if got, want := event.Parameters[StatementMetadataPhaseKey], canonicalPhasePackageRegistryEvents; got != want {
		t.Fatalf("event phase = %#v, want %#v", got, want)
	}
	eventRows, ok := event.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", event.Parameters["rows"])
	}
	if got, want := eventRows[0]["event_key"], "serial:9988"; got != want {
		t.Fatalf("event row event_key = %#v, want %#v", got, want)
	}
	if got, want := eventRows[0]["event_type"], "yank"; got != want {
		t.Fatalf("event row event_type = %#v, want %#v", got, want)
	}
	for _, fragment := range []string{
		"e.package_id = row.package_id",
		"e.version_id = row.version_id",
		"e.event_key = row.event_key",
		"e.event_type = row.event_type",
		"e.artifact_key = row.artifact_key",
		"e.actor = row.actor",
		"e.message = row.message",
		"e.occurred_at = row.occurred_at",
	} {
		if !strings.Contains(event.Cypher, fragment) {
			t.Fatalf("event Cypher = %q, want fragment %q", event.Cypher, fragment)
		}
	}

	// Edge cypher: the deferred event edge statement MUST anchor on both
	// PackageVersion and RegistryEvent and MERGE HAS_REGISTRY_EVENT.
	eventEdges := writer.buildPackageRegistryEventEdgeStatements(mat)
	if got, want := len(eventEdges), 1; got != want {
		t.Fatalf("buildPackageRegistryEventEdgeStatements() count = %d, want %d", got, want)
	}
	for _, fragment := range []string{
		"MATCH (v:PackageVersion {uid: row.version_id})",
		"MATCH (e:RegistryEvent {uid: row.uid})",
		"MERGE (v)-[rel:HAS_REGISTRY_EVENT]->(e)",
	} {
		if !strings.Contains(eventEdges[0].Cypher, fragment) {
			t.Fatalf("event EDGE Cypher = %q, want fragment %q", eventEdges[0].Cypher, fragment)
		}
	}
	if got, want := eventEdges[0].Parameters[StatementMetadataPhaseKey], canonicalPhasePackageRegistryEventEdges; got != want {
		t.Fatalf("event edge phase = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterPackageRegistryEventEdgeRunsAfterNodePhases(t *testing.T) {
	t.Parallel()

	exec := &mockPhaseGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryEvents: []projector.PackageRegistryEventRow{{
			UID:              "package-registry-event-1",
			PackageID:        "npm://registry.npmjs.org/lodash",
			VersionID:        "npm://registry.npmjs.org/lodash@1.0.0",
			EventKey:         "serial:1",
			EventType:        "yank",
			SourceFactID:     "package-registry-event-1",
			StableFactKey:    "package-registry-event-1",
			SourceSystem:     "package_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			CollectorKind:    "package_registry",
		}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	eventGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryRegistryEvent")
	eventEdgeGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryEventEdge")

	if eventEdgeGroup <= eventGroup {
		t.Fatalf(
			"package registry event edge phase must run after the event node phase: event=%d event_edge=%d",
			eventGroup,
			eventEdgeGroup,
		)
	}
}
