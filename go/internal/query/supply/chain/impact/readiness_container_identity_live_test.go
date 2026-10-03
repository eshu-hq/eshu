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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive proves
// that a mutable image reference resolves every current digest, including sets
// larger than 500, and that target resolution stays within three queries. It
// runs in a disposable database created by the live PostgreSQL readiness job.
func TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive(t *testing.T) {
	dsn := os.Getenv("ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DSN")
	optIn := os.Getenv("ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedReadinessMutableRefProof(t, ctx, db)

	args := []any{
		array.Of(vulnerabilityAdvisoryFactKinds),
		array.Of(vulnerabilityExploitabilityFactKinds),
		array.Of(packageConsumptionCorrelationFactKinds),
		array.Of(packageRegistryFactKinds),
		array.Of(sbomComponentFactKinds),
		array.Of(sbomAttestationFactKinds),
		array.Of(containerImageIdentityFactKinds),
		array.Of(vulnerabilitySourceSnapshotFactKinds),
		"", "", "", "", "", readinessMutableRef,
		array.Of(vulnerabilityOSPackageFactKinds),
		array.Of(scannerWorkerAnalysisFactKinds),
		array.Of([]string{}), // no resolved package keys in this image-only shape proof
		array.Of([]string{}),
		array.Of([]string{}),
		false,
	}
	rows, err := db.QueryContext(ctx, ListReadinessQuery, args...)
	if err != nil {
		t.Fatalf("query production readiness shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	families := make(map[string]int)
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incomplete sql.NullBool
		var reasons array.StringArray
		var sourceSnapshots, sourceStates, unsupported sql.NullString
		if err := rows.Scan(
			&family,
			&factCount,
			&latest,
			&incomplete,
			&reasons,
			&sourceSnapshots,
			&sourceStates,
			&unsupported,
		); err != nil {
			t.Fatalf("scan readiness row: %v", err)
		}
		families[family] = factCount
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read readiness rows: %v", err)
	}
	for _, family := range []string{"container_image.identity", "scanner_worker.analysis", "sbom.component"} {
		if got := families[family]; got != readinessMutableRefDigestCount {
			t.Fatalf("%s count = %d, want complete %d-digest mutable-ref set", family, got, readinessMutableRefDigestCount)
		}
	}
	if err := storagepostgres.BackfillPackageManifestConsumptionKeys(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("backfill package identity sidecars: %v", err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatalf("begin counted target resolution: %v", err)
	}
	counted := &countingReadinessQueryer{tx: tx}
	target, err := resolveReadinessTarget(ctx, readinessSQLQueryer{database: counted}, ReadinessQuery{ImageRef: readinessMutableRef})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("resolve mutable-ref target: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("close counted target resolution: %v", err)
	}
	if len(target.PackageIDs) != readinessMutableRefDigestCount {
		t.Fatalf("513-digest target resolved %d packages, want %d", len(target.PackageIDs), readinessMutableRefDigestCount)
	}
	t.Logf("513-digest target resolution queries=%d", counted.queries)
	if counted.queries > 3 {
		t.Fatalf("513-digest target resolution issued %d queries, want at most 3", counted.queries)
	}
	started := time.Now()
	snapshot, err := NewPostgresReadinessStore(db).ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{ImageRef: readinessMutableRef})
	if err != nil {
		t.Fatalf("read production mutable-ref readiness: %v", err)
	}
	t.Logf("513-digest production readiness elapsed=%s", time.Since(started))
	for _, family := range []string{EvidenceFamilyContainerImageIdentity, EvidenceFamilySBOMComponent} {
		var got int
		for _, source := range snapshot.EvidenceSources {
			if source.Family == family {
				got = source.FactCount
			}
		}
		if got != readinessMutableRefDigestCount {
			t.Fatalf("production %s count = %d, want %d", family, got, readinessMutableRefDigestCount)
		}
	}
	seedReadinessCrossScopeSBOMWarningProof(t, ctx, db)
	crossScope, err := NewPostgresReadinessStore(db).ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{
		SubjectDigest: "sha256:" + strings.Repeat("0", 63) + "1",
	})
	if err != nil {
		t.Fatalf("read cross-scope document warning: %v", err)
	}
	var crossScopeWarnings int
	for _, gap := range crossScope.UnsupportedTargets {
		if gap.TargetKind == UnsupportedTargetKindSBOMTarget && gap.Reason == "unsupported_field" {
			crossScopeWarnings += gap.Count
		}
	}
	if crossScopeWarnings != 2 {
		t.Fatalf("cross-scope same-document warning count = %d, want 2", crossScopeWarnings)
	}
}

const (
	readinessMutableRef            = "registry.example.com/team/readiness-over500:prod"
	readinessMutableRefDigestCount = 513
)

func seedReadinessMutableRefProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
)
SELECT
    'readiness-over500-scope-' || n, 'repository', 'git', 'readiness-over500-' || n,
    'git', 'readiness-over500-' || n, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, 513) AS n;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
)
SELECT
    'readiness-over500-generation-' || n, 'readiness-over500-scope-' || n,
    'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, 513) AS n;

UPDATE ingestion_scopes
SET active_generation_id = 'readiness-over500-generation-' ||
    regexp_replace(scope_id, '^readiness-over500-scope-', '')
WHERE scope_id LIKE 'readiness-over500-scope-%';

INSERT INTO container_image_identity_support_sets (set_id, scope_id, content_hash, support_count)
SELECT
    sha256(convert_to('readiness-over500-set-' || n, 'UTF8')),
    'readiness-over500-scope-' || n,
    sha256(convert_to('readiness-over500-content-' || n, 'UTF8')),
    1
FROM generate_series(1, 513) AS n;

INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
)
SELECT
    sha256(convert_to('readiness-over500-set-' || n, 'UTF8')),
    'sha256:' || lpad(to_hex(n), 64, '0'),
    sha256(convert_to('readiness-over500-support-' || n, 'UTF8')),
    'registry.example.com/team/readiness-over500:prod',
    'registry.example.com/team/readiness-over500',
    'exact_digest', 'digest', 1,
    ARRAY['repository:readiness-over500'],
    ARRAY['observed_resource', 'source_declaration']
FROM generate_series(1, 513) AS n;

UPDATE container_image_identity_scope_state AS state
SET active_set_id = sha256(convert_to(
        'readiness-over500-set-' || regexp_replace(state.scope_id, '^readiness-over500-scope-', ''),
        'UTF8'
    )),
    last_set_id = sha256(convert_to(
        'readiness-over500-set-' || regexp_replace(state.scope_id, '^readiness-over500-scope-', ''),
        'UTF8'
    )),
    last_set_hash = sha256(convert_to(
        'readiness-over500-content-' || regexp_replace(state.scope_id, '^readiness-over500-scope-', ''),
        'UTF8'
    )),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:readiness-over500', observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE state.scope_id LIKE 'readiness-over500-scope-%';

INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES (
    'readiness-over500-scan', 'container_image', 'scanner_worker', 'readiness-over500-scan',
    'scanner_worker', 'readiness-over500-scan', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES (
    'readiness-over500-scan-generation', 'readiness-over500-scan',
    'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
);
UPDATE ingestion_scopes
SET active_generation_id = 'readiness-over500-scan-generation'
WHERE scope_id = 'readiness-over500-scan';

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    'readiness-over500-analysis-' || n,
    'readiness-over500-scan',
    'readiness-over500-scan-generation',
    'scanner_worker.analysis',
    'readiness-over500-analysis-' || n,
    'scanner_worker',
    'readiness-over500-analysis-' || n,
    clock_timestamp(), clock_timestamp(), FALSE,
    jsonb_build_object(
        'image_reference', 'registry.example.com/team/readiness-over500:prod',
        'image_digest', 'sha256:' || lpad(to_hex(n), 64, '0'),
        'analysis_status', 'completed',
        'coverage_status', 'supported',
        'analyzer', 'ospkg'
    )
FROM generate_series(1, 513) AS n;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    'readiness-over500-sbom-document-' || n,
    'readiness-over500-scope-' || n,
    'readiness-over500-generation-' || n,
    'sbom.document',
    'readiness-over500-sbom-document-' || n,
    'sbom', 'readiness-over500-sbom-document-' || n,
    clock_timestamp(), clock_timestamp(), FALSE,
    jsonb_build_object(
        'document_id', 'readiness-over500-doc-' || n,
        'subject_digest', 'sha256:' || lpad(to_hex(n), 64, '0')
    )
FROM generate_series(1, 513) AS n;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    'readiness-over500-sbom-component-' || n,
    'readiness-over500-scope-' || n,
    'readiness-over500-generation-' || n,
    'sbom.component',
    'readiness-over500-sbom-component-' || n,
    'sbom', 'readiness-over500-sbom-component-' || n,
    clock_timestamp(), clock_timestamp(), FALSE,
    jsonb_build_object(
        'document_id', 'readiness-over500-doc-' || n,
        'package_id', 'npm://registry.npmjs.org/pkg-' || n,
        'name', 'pkg-' || n
    )
FROM generate_series(1, 513) AS n;

ANALYZE ingestion_scopes;
ANALYZE scope_generations;
ANALYZE container_image_identity_scope_state;
ANALYZE container_image_identity_support_sets;
ANALYZE container_image_identity_supports;
ANALYZE fact_records;
`); err != nil {
		t.Fatalf("seed mutable-ref readiness proof: %v", err)
	}
}

// seedReadinessCrossScopeSBOMWarningProof replays one source document in a
// second active scope, with its warning fact present only there.
func seedReadinessCrossScopeSBOMWarningProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES (
    'readiness-over500-unrelated-sbom', 'sbom_document', 'sbom',
    'unrelated-sbom', 'sbom', 'unrelated-sbom',
    clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES (
    'readiness-over500-unrelated-generation', 'readiness-over500-unrelated-sbom',
    'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
);
UPDATE ingestion_scopes
SET active_generation_id = 'readiness-over500-unrelated-generation'
WHERE scope_id = 'readiness-over500-unrelated-sbom'
;
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES
    ('readiness-over500-unrelated-document', 'readiness-over500-unrelated-sbom',
     'readiness-over500-unrelated-generation', 'sbom.document',
     'readiness-over500-unrelated-document', 'sbom', 'readiness-over500-unrelated-document',
     clock_timestamp(), clock_timestamp(), FALSE,
     '{"document_id":"readiness-over500-doc-1","subject_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000001"}'::jsonb),
    ('readiness-over500-unrelated-warning', 'readiness-over500-unrelated-sbom',
     'readiness-over500-unrelated-generation', 'sbom.warning',
     'readiness-over500-unrelated-warning', 'sbom', 'readiness-over500-unrelated-warning',
     clock_timestamp(), clock_timestamp(), FALSE,
     '{"document_id":"readiness-over500-doc-1","reason":"unsupported_field"}'::jsonb);
	`); err != nil {
		t.Fatalf("seed cross-scope SBOM warning: %v", err)
	}
}

type countingReadinessQueryer struct {
	tx      *sql.Tx
	queries int
}

func (q *countingReadinessQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.queries++
	return q.tx.QueryContext(ctx, query, args...)
}
