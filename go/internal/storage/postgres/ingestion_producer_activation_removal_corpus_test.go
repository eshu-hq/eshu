// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

const (
	removalDigestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	removalDigestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	removalDigestX = "sha256:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
)

// seedProducerRemovalCorpus seeds a removal-only OCI producer generation:
// removal-old carries a live manifest, removal-new (active) carries only
// that manifest's tombstone under the same stable_fact_key. One linked k8s
// consumer embeds the removed digest; one disjoint consumer embeds an
// unrelated digest and must never reopen.
func seedProducerRemovalCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('oci:removal', 'container_registry_repository', 'oci_registry', 'example/app',
		         'oci_registry', 'example/app',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'removal-new')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('removal-old', 'oci:removal', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('removal-new', 'oci:removal', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 01:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('removal-manifest-live', 'oci:removal', 'removal-old', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:removal-manifest', 'oci_registry', 'removal-manifest',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"repository_id": "example/app", "digest": "%s"}'::jsonb),
		        ('removal-manifest-tomb', 'oci:removal', 'removal-new', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:removal-manifest', 'oci_registry', 'removal-manifest',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', TRUE,
		         '{}'::jsonb)`, removalDigestA),
		`INSERT INTO producer_activation_obligations (scope_id, generation_id, state, work_item_id, created_at)
		 VALUES ('oci:removal', 'removal-new', 'pending', 'removal-obligation', clock_timestamp())`,
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('k8s:removal-linked', 'cluster', 'kubernetes', 'removal-linked',
		         'kubernetes_live', 'removal-linked',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'removal-linked'),
		        ('k8s:removal-disjoint', 'cluster', 'kubernetes', 'removal-disjoint',
		         'kubernetes_live', 'removal-disjoint',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'removal-disjoint')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('removal-linked', 'k8s:removal-linked', 'sync',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('removal-disjoint', 'k8s:removal-disjoint', 'sync',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('removal-linked-pod', 'k8s:removal-linked', 'removal-linked',
		         'kubernetes_live.pod_template', 'kubernetes_live.pod_template:removal-linked-pod',
		         'kubernetes', 'removal-linked-pod',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"object_id": "pod-removal-linked",
		           "containers": [{"name": "app", "resolved_image_digest": "example/app@%s"}]}'::jsonb),
		        ('removal-disjoint-pod', 'k8s:removal-disjoint', 'removal-disjoint',
		         'kubernetes_live.pod_template', 'kubernetes_live.pod_template:removal-disjoint-pod',
		         'kubernetes', 'removal-disjoint-pod',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"object_id": "pod-removal-disjoint",
		           "containers": [{"name": "app", "resolved_image_digest": "other/app@%s"}]}'::jsonb)`,
			removalDigestA, removalDigestX),
		`INSERT INTO fact_work_items
		    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
		 VALUES ('removal-linked/kubernetes_correlation_materialization', 'k8s:removal-linked',
		        'removal-linked', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('removal-disjoint/kubernetes_correlation_materialization', 'k8s:removal-disjoint',
		        'removal-disjoint', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer removal corpus: %v", err)
		}
	}
}

// seedProducerPartialRemovalCorpus seeds a partial-removal OCI producer
// generation: partial-old carries live manifests A and B, partial-new
// (active) keeps A live and tombstones B. Consumers embed each digest.
func seedProducerPartialRemovalCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('oci:partial', 'container_registry_repository', 'oci_registry', 'example/app',
		         'oci_registry', 'example/app',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'partial-new')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('partial-old', 'oci:partial', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('partial-new', 'oci:partial', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 01:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('partial-a-old', 'oci:partial', 'partial-old', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:partial-a', 'oci_registry', 'partial-a',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"repository_id": "example/app", "digest": "%s"}'::jsonb),
		        ('partial-b-old', 'oci:partial', 'partial-old', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:partial-b', 'oci_registry', 'partial-b',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"repository_id": "example/app", "digest": "%s"}'::jsonb),
		        ('partial-a-new', 'oci:partial', 'partial-new', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:partial-a', 'oci_registry', 'partial-a',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', FALSE,
		         '{"repository_id": "example/app", "digest": "%s"}'::jsonb),
		        ('partial-b-tomb', 'oci:partial', 'partial-new', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:partial-b', 'oci_registry', 'partial-b',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', TRUE,
		         '{}'::jsonb)`, removalDigestA, removalDigestB, removalDigestA),
		`INSERT INTO producer_activation_obligations (scope_id, generation_id, state, work_item_id, created_at)
		 VALUES ('oci:partial', 'partial-new', 'pending', 'partial-obligation', clock_timestamp())`,
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('k8s:partial-a', 'cluster', 'kubernetes', 'partial-a',
		         'kubernetes_live', 'partial-a',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'partial-a'),
		        ('k8s:partial-b', 'cluster', 'kubernetes', 'partial-b',
		         'kubernetes_live', 'partial-b',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'partial-b')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('partial-a', 'k8s:partial-a', 'sync',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('partial-b', 'k8s:partial-b', 'sync',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('partial-a-pod', 'k8s:partial-a', 'partial-a',
		         'kubernetes_live.pod_template', 'kubernetes_live.pod_template:partial-a-pod',
		         'kubernetes', 'partial-a-pod',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"object_id": "pod-partial-a",
		           "containers": [{"name": "app", "resolved_image_digest": "example/app@%s"}]}'::jsonb),
		        ('partial-b-pod', 'k8s:partial-b', 'partial-b',
		         'kubernetes_live.pod_template', 'kubernetes_live.pod_template:partial-b-pod',
		         'kubernetes', 'partial-b-pod',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"object_id": "pod-partial-b",
		           "containers": [{"name": "app", "resolved_image_digest": "example/app@%s"}]}'::jsonb)`,
			removalDigestA, removalDigestB),
		`INSERT INTO fact_work_items
		    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
		 VALUES ('partial-a/kubernetes_correlation_materialization', 'k8s:partial-a',
		        'partial-a', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('partial-b/kubernetes_correlation_materialization', 'k8s:partial-b',
		        'partial-b', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer partial-removal corpus: %v", err)
		}
	}
}

// seedProducerNoPredecessorCorpus seeds a first-generation tombstone: the
// scope's only generation tombstones a key that was never live, so no
// predecessor payload exists and the obligation must retire inapplicable.
func seedProducerNoPredecessorCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('oci:nopred', 'container_registry_repository', 'oci_registry', 'example/app',
		         'oci_registry', 'example/app',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'nopred-new')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('nopred-new', 'oci:nopred', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('nopred-tomb', 'oci:nopred', 'nopred-new', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:nopred-never-live', 'oci_registry', 'nopred-never-live',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', TRUE,
		         '{}'::jsonb)`,
		`INSERT INTO producer_activation_obligations (scope_id, generation_id, state, work_item_id, created_at)
		 VALUES ('oci:nopred', 'nopred-new', 'pending', 'nopred-obligation', clock_timestamp())`,
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer no-predecessor corpus: %v", err)
		}
	}
}

// seedProducerPodRemovalCorpus seeds a pod-template tombstone with a live
// predecessor, covering the kubernetes_live.pod_template kind in the
// removal prefilter differential. The pod refs carry image_refs only; the
// keys come from the predecessor payload, never the tombstone.
func seedProducerPodRemovalCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('oci:podremoval', 'container_registry_repository', 'oci_registry', 'example/app',
		         'oci_registry', 'example/app',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'podremoval-new')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('podremoval-old', 'oci:podremoval', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('podremoval-new', 'oci:podremoval', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 01:00:00+00')`,
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('podremoval-live', 'oci:podremoval', 'podremoval-old', 'kubernetes_live.pod_template',
		         'kubernetes_live.pod_template:podremoval-pod', 'kubernetes', 'podremoval-pod',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"object_id": "pod-podremoval", "image_refs": ["example/app:v1"]}'::jsonb),
		        ('podremoval-tomb', 'oci:podremoval', 'podremoval-new', 'kubernetes_live.pod_template',
		         'kubernetes_live.pod_template:podremoval-pod', 'kubernetes', 'podremoval-pod',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', TRUE,
		         '{}'::jsonb)`,
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer pod-removal corpus: %v", err)
		}
	}
}

// seedProducerRemovalBulk adds n unrelated manifest facts across older
// generations of the removal scope so the plan test proves the predecessor
// lookup never scans them.
func seedProducerRemovalBulk(t *testing.T, ctx context.Context, database *sql.DB, n int) {
	t.Helper()
	statements := []string{
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 SELECT 'removal-bulk-' || s, 'oci:removal', 'snapshot',
		        TIMESTAMPTZ '2026-06-01 00:00:00+00', TIMESTAMPTZ '2026-06-01 00:00:00+00',
		        'superseded', TIMESTAMPTZ '2026-06-01 00:00:00+00'
		 FROM generate_series(1, 5) AS s`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT 'removal-bulk-' || s, 'oci:removal', 'removal-bulk-' || ((s - 1) / (%d / 5) + 1),
		        'oci_registry.image_manifest',
		        'oci_registry.image_manifest:removal-bulk-' || s, 'oci_registry', 'removal-bulk-' || s,
		        TIMESTAMPTZ '2026-06-01 00:00:00+00', TIMESTAMPTZ '2026-06-01 00:00:00+00', FALSE,
		        '{"repository_id": "example/bulk", "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}'::jsonb
		 FROM generate_series(1, %d) AS s`, n, n),
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer removal bulk: %v", err)
		}
	}
}

// assertProducerObligationState requires the obligation row to sit in the
// want state.
func assertProducerObligationState(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID, want string) {
	t.Helper()
	var got string
	err := database.QueryRowContext(ctx,
		`SELECT state FROM producer_activation_obligations WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID).Scan(&got)
	if err != nil {
		t.Fatalf("read obligation state %s/%s: %v", scopeID, generationID, err)
	}
	if got != want {
		t.Fatalf("obligation state %s/%s = %q, want %q", scopeID, generationID, got, want)
	}
}

// assertProducerWorkItemStatus requires the work item to sit in the want
// status.
func assertProducerWorkItemStatus(t *testing.T, ctx context.Context, database *sql.DB, workItemID, want string) {
	t.Helper()
	var got string
	err := database.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&got)
	if err != nil {
		t.Fatalf("read work item status %s: %v", workItemID, err)
	}
	if got != want {
		t.Fatalf("work item status %s = %q, want %q", workItemID, got, want)
	}
}
