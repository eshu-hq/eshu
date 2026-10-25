// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8824 disposable PostgreSQL proof that central version pins (NuGet PackageVersion rows flagged central_version_declaration) add no package.consumption evidence on either readiness arm, so a pin-only repository reads evidence_incomplete and not ready_zero_findings, while a real reference next to a pin still counts; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestReadinessExcludesCentralVersionPinsLive is the #8824 accuracy proof for
// both package.consumption readers. A central pin sets the version a project
// gets if it references the package; it does not prove any project does. The
// repository-only arm reads facts through package_manifest_active and the
// package-anchored arm reads package_manifest_consumption_keys, so the pin
// must drop out of both, or an unused pin turns a repository with no
// dependency evidence into "ready, zero findings".
func TestReadinessExcludesCentralVersionPinsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN"))
	if dsn != "" && os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE") != "1" {
		t.Fatal("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN is set without ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=1")
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN or ESHU_POSTGRES_DSN to run the central pin readiness proof")
	}
	db := postgresproof.OpenIsolatedSchema(t, dsn, "eshu_8824_central_pins", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx := context.Background()
	// A NuGet repository whose only dependency rows are central pins.
	seedReadinessManifestScope(t, ctx, db, "nuget-pin-only", "repository:nuget-pin-only")
	seedReadinessCentralPin(t, ctx, db, "nuget-pin-only", "repository:nuget-pin-only", "nuget", "Newtonsoft.Json")
	// The same flag on an npm row, so the package-anchored key arm has a
	// registry identity to resolve. The predicate is ecosystem-blind.
	seedReadinessManifestScope(t, ctx, db, "npm-pin-only", "repository:npm-pin-only")
	seedReadinessCentralPin(t, ctx, db, "npm-pin-only", "repository:npm-pin-only", "npm", "lodash")
	// A pin next to a real reference of the same package: the reference counts.
	seedReadinessManifestScope(t, ctx, db, "pin-and-reference", "repository:pin-and-reference", "lodash")
	seedReadinessCentralPin(t, ctx, db, "pin-and-reference", "repository:pin-and-reference", "npm", "lodash")
	seedReadinessRegistryPackage(t, ctx, db, "registry-default", "npm://registry.npmjs.org/lodash")
	if err := storagepostgres.BackfillPackageManifestConsumptionKeys(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	store := NewPostgresReadinessStore(db)
	read := func(query ReadinessQuery) ReadinessSnapshot {
		t.Helper()
		snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, query)
		if err != nil {
			t.Fatalf("ReadSupplyChainImpactReadiness(%+v): %v", query, err)
		}
		return snapshot
	}
	const lodash = "npm://registry.npmjs.org/lodash"
	for _, tc := range []struct {
		name  string
		query ReadinessQuery
		want  int
	}{
		{"repository arm, pin only", ReadinessQuery{RepositoryID: "repository:nuget-pin-only"}, 0},
		{"repository arm, npm pin only", ReadinessQuery{RepositoryID: "repository:npm-pin-only"}, 0},
		{"repository arm, pin and reference", ReadinessQuery{RepositoryID: "repository:pin-and-reference"}, 1},
		{"key arm, pin only", ReadinessQuery{PackageID: lodash, RepositoryID: "repository:npm-pin-only"}, 0},
		{"key arm, pin and reference", ReadinessQuery{PackageID: lodash, RepositoryID: "repository:pin-and-reference"}, 1},
		{"key arm, package only", ReadinessQuery{PackageID: lodash}, 1},
	} {
		if got := readinessPackageConsumptionFactCount(read(tc.query)); got != tc.want {
			t.Errorf("%s: package.consumption fact count = %d, want %d", tc.name, got, tc.want)
		}
	}

	// End to end: with fresh advisory and registry evidence in place, the
	// pin-only repository must still say its owned packages are missing.
	scope := TargetScope{RepositoryID: "repository:nuget-pin-only"}
	snapshot := withFreshAdvisoryAndRegistry(read(ReadinessQuery{RepositoryID: scope.RepositoryID}))
	envelope := BuildReadiness(scope, nil, false, snapshot)
	if envelope.State != ReadinessStateEvidenceIncomplete ||
		!slices.Contains(envelope.MissingEvidence, MissingEvidenceOwnedPackages) {
		t.Fatalf("pin-only repository readiness = %q missing %v, want %q with %q",
			envelope.State, envelope.MissingEvidence, ReadinessStateEvidenceIncomplete, MissingEvidenceOwnedPackages)
	}
}

// withFreshAdvisoryAndRegistry overlays the advisory and registry evidence a
// configured deployment would have, so only package.consumption decides
// whether the repository's owned packages are proven.
func withFreshAdvisoryAndRegistry(snapshot ReadinessSnapshot) ReadinessSnapshot {
	sources := make([]EvidenceFamily, 0, len(snapshot.EvidenceSources)+2)
	for _, source := range snapshot.EvidenceSources {
		if source.Family == EvidenceFamilyVulnerabilityAdvisory || source.Family == EvidenceFamilyPackageRegistry {
			continue
		}
		sources = append(sources, source)
	}
	snapshot.EvidenceSources = append(sources,
		EvidenceFamily{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh},
		EvidenceFamily{Family: EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: FreshnessLabelFresh},
	)
	snapshot.SourceStates = []SourceState{{Source: "osv", FreshnessState: "fresh", TerminalStatus: "succeeded"}}
	return snapshot
}

// seedReadinessCentralPin writes one central version pin into the scope that
// seedReadinessManifestScope created for suffix, shaped like the row the
// NuGet Directory.Packages.props parser emits for a PackageVersion item.
func seedReadinessCentralPin(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix, repositoryID, packageManager, name string,
) {
	t.Helper()
	scopeID := "readiness-consumption-" + suffix
	factID := scopeID + "-pin-" + name
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at,
     is_tombstone, payload)
VALUES ($1, $2, $3, 'content_entity', $1, 'git', $1,
        clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', $4::text,
            'entity_type', 'Variable',
            'entity_name', $5::text,
            'entity_metadata', jsonb_build_object(
                'config_kind', 'dependency', 'package_manager', $6::text,
                'section', 'PackageVersion', 'value', '13.0.1',
                'central_version_declaration', true)))`,
		factID, scopeID, scopeID+"-gen", repositoryID, name, packageManager); err != nil {
		t.Fatalf("seed central pin %s: %v", factID, err)
	}
}
