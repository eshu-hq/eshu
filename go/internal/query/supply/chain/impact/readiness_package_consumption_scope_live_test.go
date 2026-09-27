// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactReadinessPackageConsumptionScopeLive proves that a
// package anchor counts manifest evidence only in repositories consuming that
// package. A repository anchor further narrows the count, while an unrelated
// package cannot inherit fleet-wide manifest evidence.
func TestSupplyChainImpactReadinessPackageConsumptionScopeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedReadinessManifestScope(t, ctx, db, "consuming-one", "repository:consuming-one", "lodash", "left-pad")
	seedReadinessTopLevelManifestDependency(t, ctx, db, "consuming-one", "repository:consuming-one", "top-level-only")
	seedReadinessManifestScope(t, ctx, db, "consuming-two", "repository:consuming-two", "lodash")
	seedReadinessManifestScope(t, ctx, db, "unrelated", "repository:unrelated", "chalk")
	seedReadinessAffectedPackage(t, ctx, db)
	seedReadinessAffectedPackageIDOnly(t, ctx, db)
	seedReadinessSBOMDocumentAndComponent(t, ctx, db)
	store := NewPostgresReadinessStore(db)
	if _, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{PackageID: "npm://registry.npmjs.org/lodash"}); err == nil {
		t.Fatal("ReadSupplyChainImpactReadiness() error = nil before package-manifest sidecar backfill, want unavailable coverage")
	}
	repositorySnapshot, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{RepositoryID: "repository:consuming-one"})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness() repository before backfill: %v", err)
	}
	if got := readinessPackageConsumptionFactCount(repositorySnapshot); got != 3 {
		t.Fatalf("repository package.consumption fact count before backfill = %d, want 3", got)
	}
	if err := storagepostgres.BackfillPackageManifestConsumptionKeys(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	var persistedMarker string
	if err := db.QueryRowContext(ctx, `SELECT marker_name FROM package_manifest_consumption_key_backfill_markers`).Scan(&persistedMarker); err != nil {
		t.Fatalf("load package manifest backfill marker: %v", err)
	}
	markerQueryer := &readinessMarkerArgumentQueryer{DB: db}
	ready, err := packageManifestConsumptionKeysReady(ctx, markerQueryer)
	if err != nil || !ready {
		t.Fatalf("packageManifestConsumptionKeysReady() = %t, %v; want ready", ready, err)
	}
	if len(markerQueryer.markerArgs) != 1 || markerQueryer.markerArgs[0] != persistedMarker {
		t.Fatalf("readiness marker arguments = %v, want [%q]", markerQueryer.markerArgs, persistedMarker)
	}

	for _, tc := range []struct {
		name     string
		query    ReadinessQuery
		want     int
		wantSBOM int
	}{
		{
			name:  "package consumers",
			query: ReadinessQuery{PackageID: "npm://registry.npmjs.org/lodash"},
			want:  2,
		},
		{
			name: "package and repository intersection",
			query: ReadinessQuery{
				PackageID:    "npm://registry.npmjs.org/lodash",
				RepositoryID: "repository:consuming-one",
			},
			want: 1,
		},
		{
			name:  "CVE with PURL-only affected package",
			query: ReadinessQuery{CVEID: "CVE-2026-7088"},
			want:  2,
		},
		{
			name:  "CVE with package ID only",
			query: ReadinessQuery{CVEID: "CVE-2026-7088-ID"},
			want:  2,
		},
		{
			name:     "digest through SBOM document",
			query:    ReadinessQuery{SubjectDigest: "sha256:readiness-7088"},
			want:     2,
			wantSBOM: 1,
		},
		{
			name:  "package with no consumers",
			query: ReadinessQuery{PackageID: "npm://registry.npmjs.org/absent"},
			want:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, tc.query)
			if err != nil {
				t.Fatalf("ReadSupplyChainImpactReadiness(): %v", err)
			}
			got := readinessPackageConsumptionFactCount(snapshot)
			if got != tc.want {
				t.Fatalf("package.consumption fact count = %d, want %d", got, tc.want)
			}
			if tc.wantSBOM > 0 {
				var sbomCount int
				for _, source := range snapshot.EvidenceSources {
					if source.Family == EvidenceFamilySBOMComponent {
						sbomCount = source.FactCount
					}
				}
				if sbomCount != tc.wantSBOM {
					t.Fatalf("sbom.component fact count = %d, want %d", sbomCount, tc.wantSBOM)
				}
			}
		})
	}

	seedReadinessRegistryPackage(t, ctx, db, "registry-default", "npm://registry.npmjs.org/lodash")
	seedReadinessRegistryPackage(t, ctx, db, "registry-custom", "npm://registry.example/lodash")
	if err := storagepostgres.BackfillPackageManifestConsumptionKeys(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys() after colliding registry identities: %v", err)
	}
	if _, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{PackageID: "npm://registry.npmjs.org/lodash"}); err == nil {
		t.Fatal("ReadSupplyChainImpactReadiness() error = nil with colliding registry key owners, want unavailable coverage")
	} else if !strings.Contains(err.Error(), "ownership is ambiguous or incomplete") {
		t.Fatalf("ReadSupplyChainImpactReadiness() error = %v, want ambiguous ownership", err)
	}
}

type readinessMarkerArgumentQueryer struct {
	*sql.DB
	markerArgs []any
}

func (q *readinessMarkerArgumentQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "package_manifest_consumption_key_backfill_markers") {
		q.markerArgs = append([]any(nil), args...)
	}
	return q.DB.QueryContext(ctx, query, args...)
}

func readinessPackageConsumptionFactCount(snapshot ReadinessSnapshot) int {
	for _, source := range snapshot.EvidenceSources {
		if source.Family == EvidenceFamilyPackageConsumption {
			return source.FactCount
		}
	}
	return 0
}

func seedReadinessRegistryPackage(t *testing.T, ctx context.Context, db *sql.DB, suffix, packageID string) {
	t.Helper()
	seedReadinessSourceScope(t, ctx, db, suffix)
	scopeID := "readiness-" + suffix
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at,
     is_tombstone, payload)
VALUES ($1, $2, $3, 'package_registry.package', $1, 'registry', $1,
        clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object('package_id', $4::text, 'ecosystem', 'npm',
                           'raw_name', 'lodash', 'normalized_name', 'lodash'))`,
		scopeID+"-package", scopeID, scopeID+"-gen", packageID); err != nil {
		t.Fatalf("seed registry package %s: %v", packageID, err)
	}
}

func seedReadinessAffectedPackage(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedReadinessSourceScope(t, ctx, db, "affected-package")
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at,
     is_tombstone, payload)
VALUES ('readiness-affected-package', 'readiness-affected-package',
        'readiness-affected-package-gen', 'vulnerability.affected_package',
        'readiness-affected-package', 'vulnerability_intelligence',
        'readiness-affected-package', clock_timestamp(), clock_timestamp(),
        FALSE, '{"advisory_id":"GHSA-readiness-7088",
                 "cve_id":"CVE-2026-7088", "ecosystem":"npm",
                 "package_name":"lodash", "purl":"pkg:npm/lodash@4.17.21"}'::jsonb)`); err != nil {
		t.Fatalf("seed PURL-only affected package: %v", err)
	}
}

func seedReadinessAffectedPackageIDOnly(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at,
     is_tombstone, payload)
VALUES ('readiness-affected-package-id-only', 'readiness-affected-package',
        'readiness-affected-package-gen', 'vulnerability.affected_package',
        'readiness-affected-package-id-only', 'vulnerability_intelligence',
        'readiness-affected-package-id-only', clock_timestamp(), clock_timestamp(),
        FALSE, '{"cve_id":"CVE-2026-7088-ID", "package_id":"npm://registry.npmjs.org/lodash"}'::jsonb)`); err != nil {
		t.Fatalf("seed package ID-only affected package: %v", err)
	}
}

func seedReadinessSBOMDocumentAndComponent(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedReadinessSourceScope(t, ctx, db, "sbom")
	for _, row := range []struct {
		factID string
		kind   string
		body   string
	}{
		{"readiness-sbom-document", "sbom.document", `{"document_id":"readiness-doc-7088","subject_digest":"sha256:readiness-7088"}`},
		{"readiness-sbom-component", "sbom.component", `{"document_id":"readiness-doc-7088","package_id":"npm://registry.npmjs.org/lodash","purl":"pkg:npm/lodash@4.17.21","name":"lodash"}`},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at,
     is_tombstone, payload)
VALUES ($1, 'readiness-sbom', 'readiness-sbom-gen', $2, $1,
        'sbom', $1, clock_timestamp(), clock_timestamp(), FALSE, $3::jsonb)`, row.factID, row.kind, row.body); err != nil {
			t.Fatalf("seed %s: %v", row.kind, err)
		}
	}
}

func seedReadinessSourceScope(t *testing.T, ctx context.Context, db *sql.DB, suffix string) {
	t.Helper()
	scopeID := "readiness-" + suffix
	generationID := scopeID + "-gen"
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'source', 'test', $1, 'test', $1,
        clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, scopeID); err != nil {
		t.Fatalf("seed %s scope: %v", suffix, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'sync', clock_timestamp(), clock_timestamp(), 'active')`, scopeID, generationID); err != nil {
		t.Fatalf("seed %s generation: %v", suffix, err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1",
		scopeID, generationID); err != nil {
		t.Fatalf("activate %s generation: %v", suffix, err)
	}
}

func seedReadinessManifestScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix, repositoryID string,
	names ...string,
) {
	t.Helper()
	scopeID := "readiness-consumption-" + suffix
	generationID := scopeID + "-gen"
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $2, 'git', $2,
        clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, scopeID, repositoryID); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'sync', clock_timestamp(), clock_timestamp(), 'active')`, scopeID, generationID); err != nil {
		t.Fatalf("seed generation %s: %v", generationID, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID); err != nil {
		t.Fatalf("activate generation %s: %v", generationID, err)
	}
	for _, name := range names {
		factID := scopeID + "-" + name
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
                'config_kind', 'dependency', 'package_manager', 'npm')))`,
			factID, scopeID, generationID, repositoryID, name); err != nil {
			t.Fatalf("seed manifest %s: %v", factID, err)
		}
	}
}

func seedReadinessTopLevelManifestDependency(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix, repositoryID, packageName string,
) {
	t.Helper()
	scopeID := "readiness-consumption-" + suffix
	generationID := scopeID + "-gen"
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
            'config_kind', 'dependency',
            'entity_metadata', jsonb_build_object('package_manager', 'npm')))`,
		scopeID+"-top-level-"+packageName, scopeID, generationID, repositoryID, packageName); err != nil {
		t.Fatalf("seed top-level manifest dependency %s: %v", packageName, err)
	}
}
