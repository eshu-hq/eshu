// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPostgresPackageCorrelationWriterPersistsOwnershipAndConsumptionFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresPackageCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WritePackageCorrelations(context.Background(), PackageCorrelationWrite{
		IntentID:     "intent-package",
		ScopeID:      "scope-package",
		GenerationID: "generation-package",
		SourceSystem: "package_registry",
		Cause:        "package source hints observed",
		OwnershipDecisions: []PackageSourceCorrelationDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				HintKind:        "repository",
				SourceURL:       "https://github.com/acme/team-api",
				RepositoryID:    "repo-team-api",
				RepositoryName:  "team-api",
				Outcome:         PackageSourceCorrelationExact,
				Reason:          "source hint matches repository remote exactly",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
			},
		},
		ConsumptionDecisions: []PackageConsumptionDecision{
			{
				PackageID:        "pkg:npm://registry.example/team-api",
				Ecosystem:        "npm",
				PackageName:      "team-api",
				RepositoryID:     "repo-web",
				RepositoryName:   "web",
				RelativePath:     "package.json",
				ManifestSection:  "dependencies",
				DependencyRange:  "^1.2.0",
				DependencyScope:  "runtime",
				PrivateAssets:    "all",
				DevelopmentOnly:  true,
				DependencyPath:   []string{"platform-api", "team-api"},
				DependencyDepth:  2,
				DirectDependency: boolPtr(false),
				Outcome:          PackageConsumptionManifestDeclared,
				Reason:           "git manifest dependency matches package registry identity",
				CanonicalWrites:  1,
				EvidenceFactIDs:  []string{"dep-fact"},
			},
		},
		PublicationDecisions: []PackagePublicationDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				VersionID:       "pkg:npm://registry.example/team-api@1.2.0",
				Version:         "1.2.0",
				RepositoryID:    "repo-team-api",
				RepositoryName:  "team-api",
				SourceURL:       "https://github.com/acme/team-api",
				Outcome:         PackageSourceCorrelationExact,
				Reason:          "source hint matches repository remote exactly",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
				EvidenceFactIDs: []string{"package-version-fact"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.FactsWritten, 3; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}
	// One ExecContext call for three decisions across ownership, consumption,
	// and publication: proves the batched path replaced the retired
	// three-loop per-decision dispatch.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (batched insert)", got, want)
	}
	if !strings.Contains(db.execs[0].query, "schema_version") {
		t.Fatalf("query missing schema_version column for governed package correlation fact: %s", db.execs[0].query)
	}
	rows := decodeBatchedVersionedFactCalls(t, db.execs)
	if got, want := len(rows), 3; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	for i, row := range rows {
		if got, want := row.SchemaVersion, facts.ReducerDerivedSchemaVersionV1; got != want {
			t.Fatalf("row %d schema_version = %v, want %v", i, got, want)
		}
	}
	ownershipPayload := unmarshalPackageCorrelationPayload(t, rows[0].Payload)
	if got, want := ownershipPayload["correlation_kind"], packageOwnershipCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := ownershipPayload["relationship_kind"], "ownership"; got != want {
		t.Fatalf("relationship_kind = %#v, want %#v", got, want)
	}
	if got, want := ownershipPayload["provenance_only"], true; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
	consumptionPayload := unmarshalPackageCorrelationPayload(t, rows[1].Payload)
	if got, want := consumptionPayload["correlation_kind"], packageConsumptionCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["canonical_writes"], float64(1); got != want {
		t.Fatalf("canonical_writes = %#v, want %#v", got, want)
	}
	if got, want := packageCorrelationStringSliceFromAny(consumptionPayload["dependency_path"]), []string{"platform-api", "team-api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency_path = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["dependency_depth"], float64(2); got != want {
		t.Fatalf("dependency_depth = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["direct_dependency"], false; got != want {
		t.Fatalf("direct_dependency = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["dependency_scope"], "runtime"; got != want {
		t.Fatalf("dependency_scope = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["private_assets"], "all"; got != want {
		t.Fatalf("private_assets = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["development_dependency"], true; got != want {
		t.Fatalf("development_dependency = %#v, want %#v", got, want)
	}
	publicationPayload := unmarshalPackageCorrelationPayload(t, rows[2].Payload)
	if got, want := publicationPayload["correlation_kind"], packagePublicationCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["relationship_kind"], "publication"; got != want {
		t.Fatalf("relationship_kind = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["version_id"], "pkg:npm://registry.example/team-api@1.2.0"; got != want {
		t.Fatalf("version_id = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["provenance_only"], true; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
}
