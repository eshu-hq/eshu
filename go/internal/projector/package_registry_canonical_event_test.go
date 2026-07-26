// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds the package_registry.registry_event-specific canonical
// extraction tests and fixture, split out of package_registry_canonical_test.go
// to stay under the package's 500-line-per-file convention (mirrors
// package_registry_canonical_artifact_test.go's split for #5458's
// package_artifact promotion). The shared package/version/dependency fixtures
// and scope/generation helpers (packageRegistryScope, packageRegistryGeneration,
// packageRegistryFacts, packageRegistryPackageID, packageRegistryVersionID,
// packageRegistryPublishedAt) stay in package_registry_canonical_test.go and
// are used here unqualified since both files are in package projector.

func TestBuildCanonicalMaterializationExtractsPackageRegistryEvents(t *testing.T) {
	t.Parallel()

	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), packageRegistryEventFact()),
	)

	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %+v, want none", quarantined)
	}
	if got, want := len(result.PackageRegistryEvents), 1; got != want {
		t.Fatalf("len(PackageRegistryEvents) = %d, want %d", got, want)
	}
	event := result.PackageRegistryEvents[0]
	if got, want := event.UID, "package-registry-event-1"; got != want {
		t.Fatalf("event UID = %q, want %q", got, want)
	}
	if got, want := event.PackageID, packageRegistryPackageID(); got != want {
		t.Fatalf("event PackageID = %q, want %q", got, want)
	}
	if got, want := event.VersionID, packageRegistryVersionID(); got != want {
		t.Fatalf("event VersionID = %q, want %q", got, want)
	}
	if got, want := event.EventKey, "serial:9988"; got != want {
		t.Fatalf("event EventKey = %q, want %q", got, want)
	}
	if got, want := event.EventType, "yank"; got != want {
		t.Fatalf("event EventType = %q, want %q", got, want)
	}
	if got, want := event.Actor, "registry-admin"; got != want {
		t.Fatalf("event Actor = %q, want %q", got, want)
	}
	if got, want := event.Message, "yanked for CVE-2026-1234"; got != want {
		t.Fatalf("event Message = %q, want %q", got, want)
	}
	if !event.OccurredAt.Equal(time.Date(2026, time.June, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("event OccurredAt = %s, want 2026-06-01T09:30:00Z", event.OccurredAt)
	}
}

func TestBuildCanonicalMaterializationQuarantinesPackageRegistryEventMissingIdentity(t *testing.T) {
	t.Parallel()

	eventFact := packageRegistryEventFact()
	delete(eventFact.Payload, "event_key")
	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), eventFact),
	)

	if got := len(result.PackageRegistryEvents); got != 0 {
		t.Fatalf("len(PackageRegistryEvents) = %d, want 0 for missing event_key", got)
	}
	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("len(quarantined) = %d, want %d", got, want)
	}
	if got, want := quarantined[0].factID, "package-registry-event-1"; got != want {
		t.Fatalf("quarantined[0].factID = %q, want %q", got, want)
	}
	if got, want := quarantined[0].field, "event_key"; got != want {
		t.Fatalf("quarantined[0].field = %q, want %q", got, want)
	}
}

// TestBuildCanonicalMaterializationSkipsPackageRegistryEventMissingVersionID
// proves a registry-wide event (no version_id -- the registry did not scope
// this event to a single package version) is a VALID decode that the row
// builder's own identity gate still drops rather than quarantining: the
// schema declares version_id nullable (registry-wide events are legal input),
// but this row's whole purpose is a per-VERSION yank/deprecate timeline, so a
// row with no version to anchor on is not materializable. This mirrors
// packageRegistryArtifactRow's requirement that package_id/version_id/
// artifact_key all be present-and-non-blank before a row materializes.
func TestBuildCanonicalMaterializationSkipsPackageRegistryEventMissingVersionID(t *testing.T) {
	t.Parallel()

	eventFact := packageRegistryEventFact()
	delete(eventFact.Payload, "version_id")
	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), eventFact),
	)

	if got := len(result.PackageRegistryEvents); got != 0 {
		t.Fatalf("len(PackageRegistryEvents) = %d, want 0 for missing version_id", got)
	}
	if got := len(quarantined); got != 0 {
		t.Fatalf("quarantined = %d, want 0 -- an absent version_id is a valid decode (registry-wide event), not a dead-letter", got)
	}
}

func TestBuildCanonicalMaterializationSkipsUnstablePackageRegistryEvent(t *testing.T) {
	t.Parallel()

	eventFact := packageRegistryEventFact()
	eventFact.StableFactKey = ""
	eventFact.FactID = "ephemeral-package-registry-event-1"
	result, _ := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), eventFact),
	)

	if got := len(result.PackageRegistryEvents); got != 0 {
		t.Fatalf("len(PackageRegistryEvents) = %d, want 0 for missing stable fact key", got)
	}
}

func packageRegistryEventFact() facts.Envelope {
	return facts.Envelope{
		FactID:           "package-registry-event-1",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistryRegistryEventFactKind,
		StableFactKey:    "package-registry-event-1",
		SchemaVersion:    facts.PackageRegistryRegistryEventSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 1, 9, 30, 0, 0, time.UTC),
		Payload: map[string]any{
			"collector_instance_id": "package-registry-collector-1",
			"ecosystem":             "npm",
			"registry":              "https://registry.npmjs.org",
			"package_id":            packageRegistryPackageID(),
			"version_id":            packageRegistryVersionID(),
			"version":               "1.2.3",
			"event_key":             "serial:9988",
			"event_type":            "yank",
			"artifact_key":          "pkg-1.2.3.tgz",
			"actor":                 "registry-admin",
			"message":               "yanked for CVE-2026-1234",
			"occurred_at":           "2026-06-01T09:30:00Z",
			"correlation_anchors": []any{
				packageRegistryPackageID(),
				packageRegistryVersionID(),
				"serial:9988",
			},
		},
		SourceRef: facts.Ref{
			SourceSystem:   "package_registry",
			ScopeID:        "package-registry-scope-1",
			GenerationID:   "package-registry-generation-1",
			SourceRecordID: packageRegistryVersionID() + "#serial:9988",
		},
	}
}

// TestBuildCanonicalMaterializationDropsBlankRegistryEventIdentity pins the
// event_key/event_type identity gate. factschema rejects an ABSENT required
// key but accepts a present-but-blank one, so a whitespace-only event_key
// would otherwise build a RegistryEvent whose uid carries a blank identity,
// and a whitespace-only event_type would materialize an unclassifiable node on
// a timeline whose entire purpose is the classification. Neither is a decode
// failure, so neither dead-letters — the row builder drops them, exactly as it
// drops a blank package_id/version_id.
//
// This fails if the trim-and-gate is removed: the row materializes with the
// untrimmed blank value instead of being dropped.
func TestBuildCanonicalMaterializationDropsBlankRegistryEventIdentity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		field string
		value string
	}{
		{name: "blank event_key", field: "event_key", value: "   "},
		{name: "blank event_type", field: "event_type", value: "\t"},
		{name: "empty event_key", field: "event_key", value: ""},
		{name: "empty event_type", field: "event_type", value: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fact := packageRegistryEventFact()
			payload := make(map[string]any, len(fact.Payload))
			for k, v := range fact.Payload {
				payload[k] = v
			}
			payload[tc.field] = tc.value
			fact.Payload = payload

			result, quarantined := buildCanonicalMaterialization(
				packageRegistryScope(),
				packageRegistryGeneration(),
				append(packageRegistryFacts(), fact),
			)

			if len(result.PackageRegistryEvents) != 0 {
				t.Fatalf("len(PackageRegistryEvents) = %d, want 0: a blank %s must not materialize a RegistryEvent", len(result.PackageRegistryEvents), tc.field)
			}
			if len(quarantined) != 0 {
				t.Fatalf("len(quarantined) = %d, want 0: a present-but-blank %s is a valid decode, not a dead-letter", len(quarantined), tc.field)
			}
		})
	}
}
