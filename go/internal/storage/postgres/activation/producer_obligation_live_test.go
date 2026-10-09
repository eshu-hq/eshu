// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestProducerActivationQuietAckLeavesConsumerUnreplayedLive is the #7635
// fixture: an OCI producer scope activates a new generation with no later
// commit, and a succeeded kubernetes_correlation_materialization item
// computed against the producer's old generation must be replayed. Every
// producer commit, Claim and Ack is the production store path on the full
// bootstrap schema; the consumer scope is seeded. The consumer item is
// seeded after the old generation's obligation settles with no dependents,
// so the new generation's obligation is the only trigger that can replay
// it. RED on main: nothing replays the consumer after a quiet Ack.
func TestProducerActivationQuietAckLeavesConsumerUnreplayedLive(t *testing.T) {
	database := openProducerActivationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.Now = func() time.Time { return base.Add(2 * time.Minute) }
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "producer-projector", time.Minute)
	queue.Now = func() time.Time { return base.Add(3 * time.Minute) }

	producerScope := scope.IngestionScope{
		ScopeID:       "oci:producer",
		SourceSystem:  "oci_registry",
		ScopeKind:     scope.KindContainerRegistryRepository,
		CollectorKind: scope.CollectorOCIRegistry,
		PartitionKey:  "example/app",
	}
	if err := store.CommitScopeGeneration(ctx, producerScope,
		testfixtures.CatalogGeneration("oci:producer", "producer-old", base),
		testfixtures.FactChannel(producerOCIFacts("oci:producer", "producer-old",
			"sha256:1111111111111111111111111111111111111111111111111111111111111111", "", base))); err != nil {
		t.Fatalf("commit producer old generation: %v", err)
	}
	claimAckQuiet(t, ctx, queue, "oci:producer", "producer-old")
	// The old obligation settles with no consumer in the store: nothing to
	// reopen, and the obligation must complete rather than wedge.
	drained, err := postgres.SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle old producer activation: %v", err)
	}
	if len(drained) != 0 {
		t.Fatalf("settle old producer activation reopened %v, want nothing", drained)
	}

	// The consumer's pod runs the old digest behind tag v1; its succeeded
	// item was computed when the producer's old generation was active.
	seedSucceededProducerConsumer(t, ctx, database, base.Add(2*time.Minute))

	// The producer tag moves to a new digest. The commit persists the
	// pending generation and the Ack activates it; nothing commits and no
	// maintenance runs after this Ack.
	if err := store.CommitScopeGeneration(ctx, producerScope,
		testfixtures.CatalogGeneration("oci:producer", "producer-new", base.Add(time.Minute)),
		testfixtures.FactChannel(producerOCIFacts("oci:producer", "producer-new",
			"sha256:2222222222222222222222222222222222222222222222222222222222222222",
			"sha256:1111111111111111111111111111111111111111111111111111111111111111", base.Add(time.Minute)))); err != nil {
		t.Fatalf("commit producer new generation: %v", err)
	}
	claimAckQuiet(t, ctx, queue, "oci:producer", "producer-new")

	reopened, err := postgres.SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle producer activations: %v", err)
	}
	if reopened["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("settle reopened %v, want one kubernetes_correlation_materialization item", reopened)
	}
	var status string
	if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`,
		"consumer-current/kubernetes_correlation_materialization").Scan(&status); err != nil {
		t.Fatalf("read consumer item status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("consumer item status after producer settle = %q, want pending (replayed)", status)
	}
}

// openProducerActivationProofDB opens an isolated schema with the full
// production bootstrap applied, on the deferred-partition proof DSN.
func openProducerActivationProofDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	return openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_activation")
}

// producerOCIFacts is one generation's OCI evidence: the manifest at digest
// plus the tag observation pointing v1 at it (with previousDigest when the
// tag moved). repository_id carries no registry prefix so the fixture's
// repository key matches the consumer pod's plain image reference.
func producerOCIFacts(scopeID, generationID, digest, previousDigest string, at time.Time) []facts.Envelope {
	manifest := facts.Envelope{
		FactID: generationID + "-manifest", ScopeID: scopeID, GenerationID: generationID,
		FactKind:      "oci_registry.image_manifest",
		StableFactKey: "oci_registry.image_manifest:" + generationID + "-manifest",
		ObservedAt:    at,
		Payload:       map[string]any{"repository_id": "example/app", "digest": digest},
		SourceRef:     facts.Ref{SourceSystem: "oci_registry", FactKey: generationID + "-manifest"},
	}
	tagPayload := map[string]any{"repository_id": "example/app", "tag": "v1", "resolved_digest": digest}
	if previousDigest != "" {
		tagPayload["previous_digest"] = previousDigest
	}
	tag := facts.Envelope{
		FactID: generationID + "-tag", ScopeID: scopeID, GenerationID: generationID,
		FactKind:      "oci_registry.image_tag_observation",
		StableFactKey: "oci_registry.image_tag_observation:" + generationID + "-tag",
		ObservedAt:    at,
		Payload:       tagPayload,
		SourceRef:     facts.Ref{SourceSystem: "oci_registry", FactKey: generationID + "-tag"},
	}
	return []facts.Envelope{manifest, tag}
}

// seedSucceededProducerConsumer inserts the active k8s consumer scope: a pod
// running the old digest behind tag v1 plus the succeeded
// kubernetes_correlation_materialization item the quiet activation must
// replay.
func seedSucceededProducerConsumer(t *testing.T, ctx context.Context, database *sql.DB, at time.Time) {
	t.Helper()
	steps := []string{
		`
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ('k8s:consumer', 'cluster', 'kubernetes', 'consumer', 'kubernetes_live', 'consumer',
    $1, $1, 'active', 'consumer-current')`, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('consumer-current', 'k8s:consumer', 'sync', $1, $1, 'active', $1)`, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('consumer-pod', 'k8s:consumer', 'consumer-current', 'kubernetes_live.pod_template',
    'kubernetes_live.pod_template:consumer-pod', 'kubernetes', 'consumer-pod',
    $1, $1, FALSE,
    '{"object_id": "pod-consumer-1", "image_refs": ["example/app:v1"],
      "containers": [{"name": "app", "image": "example/app:v1",
        "resolved_image_digest": "example/app@sha256:1111111111111111111111111111111111111111111111111111111111111111"}]}'::jsonb)`, `
INSERT INTO fact_work_items
    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ('consumer-current/kubernetes_correlation_materialization', 'k8s:consumer',
    'consumer-current', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1, $1, $1)`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step, at); err != nil {
			t.Fatalf("seed consumer scope step %d: %v", i, err)
		}
	}
}
