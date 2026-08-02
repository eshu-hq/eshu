// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func seedContainerImageIdentityV3MigrationRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    'repository:5740-v3-upgrade', 'repository', 'git', 'synthetic-5740-v3-upgrade',
    'git', 'synthetic-5740-v3-upgrade', clock_timestamp(), clock_timestamp(), 'active'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES (
    'generation:5740-v3-upgrade', 'repository:5740-v3-upgrade', 'synthetic',
    clock_timestamp(), clock_timestamp(), 'active'
);
UPDATE ingestion_scopes
SET active_generation_id = 'generation:5740-v3-upgrade'
WHERE scope_id = 'repository:5740-v3-upgrade';
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, payload, created_at, updated_at
)
SELECT
    'intent:5740-v3-upgrade:' || series,
    'repository:5740-v3-upgrade', 'generation:5740-v3-upgrade',
    'reducer', 'container_image_identity', 'intent',
    'intent:5740-v3-upgrade:' || series, 'pending',
    jsonb_build_object('source_system', 'git'), clock_timestamp(), clock_timestamp()
FROM generate_series(1, 10000) AS series;
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
    'legacy:5740-v3-upgrade:' || series,
    'repository:5740-v3-upgrade', 'generation:5740-v3-upgrade',
    'reducer_container_image_identity', 'container_image_identity:legacy:' || series,
    'reducer', 'git', 'intent:5740-v3-upgrade:' || series,
    clock_timestamp(), clock_timestamp(),
    jsonb_build_object(
        'digest', 'sha256:' || lpad(to_hex(series), 64, '0'),
        'image_ref', 'registry.example.com/performance/image-' || series ||
            '@sha256:' || lpad(to_hex(series), 64, '0'),
        'repository_id', 'oci-registry://registry.example.com/performance/image-' || series,
        'outcome', 'exact_digest', 'identity_strength', 'immutable_digest',
        'canonical_writes', 1,
        'source_repository_ids', jsonb_build_array('repository:5740-v3-upgrade')
    )
FROM generate_series(1, 10000) AS series;
`); err != nil {
		t.Fatalf("seed populated digest-v3 migration shape: %v", err)
	}
}

func proveContainerImageIdentityV3FirstHeldPublication(
	t *testing.T, ctx context.Context, db *sql.DB, activationEpoch int64,
) {
	t.Helper()
	const (
		scopeID      = "repository:5740-v3-upgrade"
		generationID = "generation:5740-v3-upgrade"
		intentID     = "intent:5740-v3-upgrade:1"
	)
	digest := fmt.Sprintf("sha256:%064x", 1)
	imageRef := "registry.example.com/performance/image-1@" + digest
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', attempt_count = 1,
    lease_owner = 'reducer-5740-v3-upgrade',
    claim_until = clock_timestamp() + interval '1 minute',
    container_image_identity_claim_epoch = 1,
    container_image_identity_v2_required = TRUE,
    container_image_identity_v2_authorized_status = 'claimed',
    container_image_identity_v3_required = TRUE,
    container_image_identity_v3_authorized_status = 'claimed'
WHERE work_item_id = $1
`, intentID); err != nil {
		t.Fatalf("claim first held v3 publication: %v", err)
	}
	queryDB := SQLDB{DB: db}
	writer := reducer.PostgresContainerImageIdentitySupportWriter{
		ActivationLookup:  NewContainerImageIdentityScopeStateStore(queryDB),
		HeldSupportLoader: NewContainerImageIdentityHeldSupportStore(queryDB),
		ClaimedExecer:     ContainerImageIdentityClaimedExecer{DB: queryDB},
	}
	held := reducer.ContainerImageIdentityDecision{
		ImageRef: imageRef,
		Outcome:  reducer.ContainerImageIdentityUnresolved,
	}
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, reducer.ContainerImageIdentityWrite{
		IntentID:        intentID,
		ClaimEpoch:      1,
		ActivationEpoch: activationEpoch,
		ScopeID:         scopeID,
		GenerationID:    generationID,
		SourceSystem:    "git",
		Cause:           "synthetic held first publication",
		EvidenceAsOf:    time.Now().UTC(),
		Decisions:       []reducer.ContainerImageIdentityDecision{held},
		HeldDecisions:   []reducer.ContainerImageIdentityDecision{held},
	})
	if err != nil {
		t.Fatalf("first held v3 publication: %v", err)
	}
	if result.CanonicalWrites != 0 || result.LegacyRowsDeleted != containerImageIdentityV3MigrationRows {
		t.Fatalf("first held result canonical/deleted = %d/%d, want 0/%d", result.CanonicalWrites, result.LegacyRowsDeleted, containerImageIdentityV3MigrationRows)
	}
	var legacyRows, supportSets, supports, visibleRows int
	if err := db.QueryRowContext(ctx, `
SELECT
    (SELECT count(*) FROM fact_records
     WHERE scope_id = $1 AND generation_id = $2
       AND fact_kind = 'reducer_container_image_identity'),
    (SELECT count(*) FROM container_image_identity_support_sets WHERE scope_id = $1),
    (SELECT count(*) FROM container_image_identity_supports),
    (SELECT count(*) FROM container_image_identity_current_supports
     WHERE scope_id = $1 AND digest = $3)
`, scopeID, generationID, digest).Scan(&legacyRows, &supportSets, &supports, &visibleRows); err != nil {
		t.Fatalf("read first held v3 publication: %v", err)
	}
	if legacyRows != 0 || supportSets != 1 || supports != 1 || visibleRows != 1 {
		t.Fatalf("first held legacy/sets/supports/visible = %d/%d/%d/%d, want 0/1/1/1", legacyRows, supportSets, supports, visibleRows)
	}
}
