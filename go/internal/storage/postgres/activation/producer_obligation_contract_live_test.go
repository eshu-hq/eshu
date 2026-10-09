// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// producerContractFixture is one #7635 contract proof's stores: the producer
// OCI scope from the RED fixture plus a projector queue driving quiet Acks.
type producerContractFixture struct {
	database *sql.DB
	ctx      context.Context
	base     time.Time
	store    postgres.IngestionStore
	queue    postgres.ProjectorQueue
	producer scope.IngestionScope
}

func openProducerContractFixture(t *testing.T) *producerContractFixture {
	t.Helper()
	database := openProducerActivationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	base := time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.Now = func() time.Time { return base.Add(2 * time.Minute) }
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "producer-projector", time.Minute)
	queue.Now = func() time.Time { return base.Add(3 * time.Minute) }
	return &producerContractFixture{
		database: database,
		ctx:      ctx,
		base:     base,
		store:    store,
		queue:    queue,
		producer: scope.IngestionScope{
			ScopeID:       "oci:producer",
			SourceSystem:  "oci_registry",
			ScopeKind:     scope.KindContainerRegistryRepository,
			CollectorKind: scope.CollectorOCIRegistry,
			PartitionKey:  "example/app",
		},
	}
}

// commitProducerGeneration commits one OCI producer generation whose tag v1
// points at digest (recording previousDigest when the tag moved).
func (f *producerContractFixture) commitProducerGeneration(t *testing.T, generationID, digest, previousDigest string, at time.Time) {
	t.Helper()
	if err := f.store.CommitScopeGeneration(f.ctx, f.producer,
		testfixtures.CatalogGeneration("oci:producer", generationID, at),
		testfixtures.FactChannel(producerOCIFacts("oci:producer", generationID, digest, previousDigest, at))); err != nil {
		t.Fatalf("commit producer generation %s: %v", generationID, err)
	}
	claimAckQuiet(t, f.ctx, f.queue, "oci:producer", generationID)
}

// producerObligationState reads one obligation row's state and lease owner.
func producerObligationState(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) (string, string) {
	t.Helper()
	var (
		state    string
		leaseOwn sql.NullString
	)
	if err := database.QueryRowContext(ctx,
		`SELECT state, lease_owner FROM producer_activation_obligations WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID).Scan(&state, &leaseOwn); err != nil {
		t.Fatalf("read producer obligation %s/%s: %v", scopeID, generationID, err)
	}
	return state, leaseOwn.String
}

// producerWorkItemStatus reads one work item's status.
func producerWorkItemStatus(t *testing.T, ctx context.Context, database *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := database.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("read work item %s: %v", workItemID, err)
	}
	return status
}

const (
	producerOldDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	producerNewDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// TestProducerActivationSettleIsExactlyOnceLive proves the #7635 settle is
// exactly-once per producer activation: the first settle reopens the linked
// consumer and completes the obligation, and a second settle claims nothing
// and writes nothing.
func TestProducerActivationSettleIsExactlyOnceLive(t *testing.T) {
	f := openProducerContractFixture(t)
	f.commitProducerGeneration(t, "producer-old", producerOldDigest, "", f.base)
	if _, err := postgres.SettleProducerActivations(f.ctx, f.database); err != nil {
		t.Fatalf("settle old producer activation: %v", err)
	}
	seedSucceededProducerConsumer(t, f.ctx, f.database, f.base.Add(2*time.Minute))
	f.commitProducerGeneration(t, "producer-new", producerNewDigest, producerOldDigest, f.base.Add(time.Minute))

	first, err := postgres.SettleProducerActivations(f.ctx, f.database)
	if err != nil {
		t.Fatalf("first settle: %v", err)
	}
	if first["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("first settle reopened %v, want one kubernetes_correlation_materialization item", first)
	}
	if state, _ := producerObligationState(t, f.ctx, f.database, "oci:producer", "producer-new"); state != "completed" {
		t.Fatalf("obligation state after first settle = %q, want completed", state)
	}

	second, err := postgres.SettleProducerActivations(f.ctx, f.database)
	if err != nil {
		t.Fatalf("second settle: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second settle reopened %v, want nothing", second)
	}
	if state, _ := producerObligationState(t, f.ctx, f.database, "oci:producer", "producer-new"); state != "completed" {
		t.Fatalf("obligation state after second settle = %q, want completed", state)
	}
	if got := producerWorkItemStatus(t, f.ctx, f.database,
		"consumer-current/kubernetes_correlation_materialization"); got != "pending" {
		t.Fatalf("consumer item status after second settle = %q, want pending", got)
	}
}

// TestProducerActivationLeaseFencesStaleSettleLive proves a settle under an
// expired lease writes nothing: owner A claims, its lease expires, owner B
// reclaims (bumping the token), and A's settle attempt leaves the consumer
// succeeded and the obligation leased to B. B's settle then replays and
// completes.
func TestProducerActivationLeaseFencesStaleSettleLive(t *testing.T) {
	f := openProducerContractFixture(t)
	f.commitProducerGeneration(t, "producer-old", producerOldDigest, "", f.base)
	if _, err := postgres.SettleProducerActivations(f.ctx, f.database); err != nil {
		t.Fatalf("settle old producer activation: %v", err)
	}
	seedSucceededProducerConsumer(t, f.ctx, f.database, f.base.Add(2*time.Minute))
	f.commitProducerGeneration(t, "producer-new", producerNewDigest, producerOldDigest, f.base.Add(time.Minute))

	obligations := activation.NewStore(postgres.SQLDB{DB: f.database})
	stale, err := obligations.ClaimProducerActivation(f.ctx, "owner-a", time.Minute)
	if err != nil || stale == nil {
		t.Fatalf("claim as owner-a: claimed=%v err=%v", stale != nil, err)
	}
	if _, err := f.database.ExecContext(f.ctx,
		`UPDATE producer_activation_obligations SET lease_until = clock_timestamp() - interval '1 second'
		 WHERE scope_id = 'oci:producer' AND generation_id = 'producer-new'`); err != nil {
		t.Fatalf("expire owner-a lease: %v", err)
	}
	current, err := obligations.ClaimProducerActivation(f.ctx, "owner-b", time.Minute)
	if err != nil || current == nil {
		t.Fatalf("reclaim as owner-b: claimed=%v err=%v", current != nil, err)
	}
	if current.LeaseToken == stale.LeaseToken {
		t.Fatalf("reclaim did not bump the token: still %d", current.LeaseToken)
	}

	staleCounts, staleOutcome, err := f.store.SettleClaimedProducerActivation(f.ctx, *stale)
	if err != nil {
		t.Fatalf("stale settle: %v", err)
	}
	if staleOutcome != activation.ProducerNotOwner {
		t.Fatalf("stale settle outcome = %q, want not_owner", staleOutcome)
	}
	if len(staleCounts) != 0 {
		t.Fatalf("stale settle reopened %v, want nothing", staleCounts)
	}
	if got := producerWorkItemStatus(t, f.ctx, f.database,
		"consumer-current/kubernetes_correlation_materialization"); got != "succeeded" {
		t.Fatalf("consumer item status after stale settle = %q, want succeeded", got)
	}
	if state, owner := producerObligationState(t, f.ctx, f.database, "oci:producer", "producer-new"); state != "leased" || owner != "owner-b" {
		t.Fatalf("obligation after stale settle = %q/%q, want leased/owner-b", state, owner)
	}

	counts, outcome, err := f.store.SettleClaimedProducerActivation(f.ctx, *current)
	if err != nil {
		t.Fatalf("owner-b settle: %v", err)
	}
	if outcome != activation.ProducerCompleted {
		t.Fatalf("owner-b settle outcome = %q, want completed", outcome)
	}
	if counts["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("owner-b settle reopened %v, want one kubernetes_correlation_materialization item", counts)
	}
	if state, _ := producerObligationState(t, f.ctx, f.database, "oci:producer", "producer-new"); state != "completed" {
		t.Fatalf("obligation state after owner-b settle = %q, want completed", state)
	}
}

// seedDisjointProducerConsumer inserts a k8s consumer scope whose pod runs an
// image with no key intersection with the fixture producer (different
// repository, tag and digest), plus its succeeded materialization item.
func seedDisjointProducerConsumer(t *testing.T, ctx context.Context, database *sql.DB, at time.Time) {
	t.Helper()
	steps := []string{
		`
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ('k8s:unrelated', 'cluster', 'kubernetes', 'unrelated', 'kubernetes_live', 'unrelated',
    $1, $1, 'active', 'unrelated-current')`, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('unrelated-current', 'k8s:unrelated', 'sync', $1, $1, 'active', $1)`, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('unrelated-pod', 'k8s:unrelated', 'unrelated-current', 'kubernetes_live.pod_template',
    'kubernetes_live.pod_template:unrelated-pod', 'kubernetes', 'unrelated-pod',
    $1, $1, FALSE,
    '{"object_id": "pod-unrelated-1", "image_refs": ["other/app:v9"],
      "containers": [{"name": "app", "image": "other/app:v9",
        "resolved_image_digest": "other/app@sha256:9999999999999999999999999999999999999999999999999999999999999999"}]}'::jsonb)`, `
INSERT INTO fact_work_items
    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ('unrelated-current/kubernetes_correlation_materialization', 'k8s:unrelated',
    'unrelated-current', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1, $1, $1)`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step, at); err != nil {
			t.Fatalf("seed disjoint consumer step %d: %v", i, err)
		}
	}
}

// TestProducerActivationUnrelatedScopeIsNotReopenedLive proves the settle is
// not a reopen storm: only the consumer whose keys intersect the owed
// generation replays, while a scope with disjoint keys stays succeeded and
// the obligation still completes.
func TestProducerActivationUnrelatedScopeIsNotReopenedLive(t *testing.T) {
	f := openProducerContractFixture(t)
	f.commitProducerGeneration(t, "producer-old", producerOldDigest, "", f.base)
	if _, err := postgres.SettleProducerActivations(f.ctx, f.database); err != nil {
		t.Fatalf("settle old producer activation: %v", err)
	}
	seedSucceededProducerConsumer(t, f.ctx, f.database, f.base.Add(2*time.Minute))
	seedDisjointProducerConsumer(t, f.ctx, f.database, f.base.Add(2*time.Minute))
	f.commitProducerGeneration(t, "producer-new", producerNewDigest, producerOldDigest, f.base.Add(time.Minute))

	reopened, err := postgres.SettleProducerActivations(f.ctx, f.database)
	if err != nil {
		t.Fatalf("settle producer activations: %v", err)
	}
	if len(reopened) != 1 || reopened["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("settle reopened %v, want exactly one kubernetes_correlation_materialization item", reopened)
	}
	if got := producerWorkItemStatus(t, f.ctx, f.database,
		"unrelated-current/kubernetes_correlation_materialization"); got != "succeeded" {
		t.Fatalf("disjoint consumer item status = %q, want succeeded", got)
	}
	if got := producerWorkItemStatus(t, f.ctx, f.database,
		"consumer-current/kubernetes_correlation_materialization"); got != "pending" {
		t.Fatalf("linked consumer item status = %q, want pending", got)
	}
}

const producerDriftARN = "arn:aws:lambda:us-east-1:123456789012:function:payments-api"

// producerTerraformFacts is one generation's Terraform evidence: the state
// resource carrying the drift ARN.
func producerTerraformFacts(scopeID, generationID, arn string, at time.Time) []facts.Envelope {
	return []facts.Envelope{{
		FactID: generationID + "-state", ScopeID: scopeID, GenerationID: generationID,
		FactKind:      "terraform_state_resource",
		StableFactKey: "terraform_state_resource:" + generationID + "-state",
		ObservedAt:    at,
		Payload: map[string]any{
			"address": "module.app.aws_lambda_function.app",
			"type":    "aws_lambda_function",
			"attributes": map[string]any{
				"arn":          arn,
				"package_type": "Image",
				"version":      "7",
			},
		},
		SourceRef: facts.Ref{SourceSystem: "terraform_state", FactKey: generationID + "-state"},
	}}
}

// seedDriftProducerConsumerScope inserts the AWS consumer scope with the
// aws_resource fact carrying the drift ARN.
func seedDriftProducerConsumerScope(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	steps := []string{
		`
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ('aws:consumer', 'account', 'aws', 'consumer', 'aws', 'consumer',
    $1, $1, 'active', 'drift-current')`, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('drift-current', 'aws:consumer', 'snapshot', $1, $1, 'active', $1)`, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('drift-resource', 'aws:consumer', 'drift-current', 'aws_resource',
    'aws_resource:drift-resource', 'aws', 'drift-resource',
    $1, $1, FALSE,
    '{"arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
      "resource_id": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
      "resource_type": "aws_lambda_function"}'::jsonb)`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step, at); err != nil {
			t.Fatalf("seed drift consumer scope step %d: %v", i, err)
		}
	}
}

// seedDriftProducerItems inserts one succeeded drift item per id, each with
// its own completion time.
func seedDriftProducerItems(t *testing.T, ctx context.Context, database *sql.DB, items map[string]time.Time) {
	t.Helper()
	for id, completed := range items {
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items
    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ($1, 'aws:consumer', 'drift-current', 'reducer', 'aws_cloud_runtime_drift', 'succeeded', 1, $2, $2)`,
			id, completed); err != nil {
			t.Fatalf("seed drift consumer item %s: %v", id, err)
		}
	}
}

// openProducerContractSchema opens one arm of the settle/epoch differential
// with its own isolated bootstrap schema.
func openProducerContractSchema(t *testing.T, prefix string) *sql.DB {
	t.Helper()
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	return openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), prefix)
}

// seedProducerContractFixture seeds both differential arms identically:
// producer-old committed and Acked with its obligation drained, linked and
// disjoint consumers succeeded, then producer-new committed and Acked with
// no maintenance after the Ack.
func seedProducerContractFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.Now = func() time.Time { return base.Add(2 * time.Minute) }
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "producer-projector", time.Minute)
	queue.Now = func() time.Time { return base.Add(3 * time.Minute) }
	producer := scope.IngestionScope{
		ScopeID:       "oci:producer",
		SourceSystem:  "oci_registry",
		ScopeKind:     scope.KindContainerRegistryRepository,
		CollectorKind: scope.CollectorOCIRegistry,
		PartitionKey:  "example/app",
	}
	commit := func(generationID, digest, previousDigest string, at time.Time) {
		t.Helper()
		if err := store.CommitScopeGeneration(ctx, producer,
			testfixtures.CatalogGeneration("oci:producer", generationID, at),
			testfixtures.FactChannel(producerOCIFacts("oci:producer", generationID, digest, previousDigest, at))); err != nil {
			t.Fatalf("commit producer generation %s: %v", generationID, err)
		}
		claimAckQuiet(t, ctx, queue, "oci:producer", generationID)
	}
	commit("producer-old", producerOldDigest, "", base)
	if _, err := postgres.SettleProducerActivations(ctx, database); err != nil {
		t.Fatalf("settle old producer activation: %v", err)
	}
	seedSucceededProducerConsumer(t, ctx, database, base.Add(2*time.Minute))
	seedDisjointProducerConsumer(t, ctx, database, base.Add(2*time.Minute))
	commit("producer-new", producerNewDigest, producerOldDigest, base.Add(time.Minute))
}

// TestProducerActivationSettleMatchesEpochPassLive is the #7635 output
// differential: after a quiet producer Ack, the producer settle (arm A) must
// replay exactly the stale linked consumer the epoch whole pass (arm B)
// replays, while the disjoint consumer shows the settle's selectivity — the
// epoch pass replays it as blanket churn and the settle leaves it
// succeeded. The settle's reopened set is a subset of the epoch pass's:
// quiet+settle converges with commit-follows+epoch for every consumer that
// needs replay and replays nothing the epoch pass would not.
func TestProducerActivationSettleMatchesEpochPassLive(t *testing.T) {
	quiet := openProducerContractSchema(t, "producer_settle_quiet")
	epoch := openProducerContractSchema(t, "producer_settle_epoch")
	seedProducerContractFixture(t, quiet)
	seedProducerContractFixture(t, epoch)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := postgres.SettleProducerActivations(ctx, quiet); err != nil {
		t.Fatalf("quiet arm settle: %v", err)
	}
	epochStore := postgres.NewIngestionStore(postgres.SQLDB{DB: epoch})
	if err := epochStore.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("epoch arm maintenance: %v", err)
	}

	const linked = "consumer-current/kubernetes_correlation_materialization"
	const disjoint = "unrelated-current/kubernetes_correlation_materialization"
	if got := producerWorkItemStatus(t, ctx, quiet, linked); got != "pending" {
		t.Fatalf("quiet arm linked consumer = %q, want pending", got)
	}
	if got := producerWorkItemStatus(t, ctx, epoch, linked); got != "pending" {
		t.Fatalf("epoch arm linked consumer = %q, want pending", got)
	}
	if got := producerWorkItemStatus(t, ctx, quiet, disjoint); got != "succeeded" {
		t.Fatalf("quiet arm disjoint consumer = %q, want succeeded", got)
	}
	if got := producerWorkItemStatus(t, ctx, epoch, disjoint); got != "pending" {
		t.Fatalf("epoch arm disjoint consumer = %q, want pending (blanket epoch replay)", got)
	}
	if state, _ := producerObligationState(t, ctx, quiet, "oci:producer", "producer-new"); state != "completed" {
		t.Fatalf("quiet arm obligation = %q, want completed", state)
	}
}

// TestProducerActivationPruneAndStatsLive proves the producer backlog
// lifecycle: the census counts every row state with the oldest open age,
// and the prune deletes only finished completed/obsolete rows past
// retention, leaving inapplicable rows (generation cascade owns them) and
// fresh rows behind.
func TestProducerActivationPruneAndStatsLive(t *testing.T) {
	database := openProducerActivationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-48 * time.Hour)
	setup := []string{
		`
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ('t:prune', 'container_registry_repository', 'oci_registry', 'prune', 'oci_registry', 'prune',
    $1, $1, 'active', 'gen-pending')`, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen-' || g, 't:prune', 'snapshot', $1, $1,
       CASE WHEN g = 'pending' THEN 'active' ELSE 'superseded' END, $1
FROM (VALUES ('pending'), ('leased'), ('completed-old'), ('completed-fresh'), ('obsolete-old'), ('inapplicable-old')) AS v(g)`,
	}
	for i, step := range setup {
		if _, err := database.ExecContext(ctx, step, now); err != nil {
			t.Fatalf("seed prune scope step %d: %v", i, err)
		}
	}
	rows := []struct {
		generation string
		state      string
		finished   *time.Time
	}{
		{"gen-pending", "pending", nil},
		{"gen-leased", "leased", nil},
		{"gen-completed-old", "completed", &old},
		{"gen-completed-fresh", "completed", &now},
		{"gen-obsolete-old", "obsolete", &old},
		{"gen-inapplicable-old", "inapplicable", &old},
	}
	for _, row := range rows {
		var finished any
		if row.finished != nil {
			finished = *row.finished
		}
		var owner, until any
		if row.state == "leased" {
			owner, until = "owner", old
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO producer_activation_obligations
    (scope_id, generation_id, state, work_item_id, lease_owner, lease_until, created_at, finished_at)
VALUES ('t:prune', $1, $2, 'prune-' || $1, $3, $4, $5, $6)`,
			row.generation, row.state, owner, until, old, finished); err != nil {
			t.Fatalf("seed producer obligation %s: %v", row.generation, err)
		}
	}

	obligations := activation.NewStore(postgres.SQLDB{DB: database})
	stats, err := obligations.StatsProducer(ctx)
	if err != nil {
		t.Fatalf("producer activation stats: %v", err)
	}
	want := map[activation.ProducerState]int64{
		activation.ProducerStatePending: 1, activation.ProducerStateLeased: 1,
		activation.ProducerStateCompleted: 2, activation.ProducerStateObsolete: 1,
		activation.ProducerStateInapplicable: 1,
	}
	if !reflect.DeepEqual(stats.ByState, want) {
		t.Fatalf("stats by state = %v, want %v", stats.ByState, want)
	}
	if stats.OldestOpenAge <= 0 {
		t.Fatalf("oldest open age = %s, want positive", stats.OldestOpenAge)
	}

	pruned, err := obligations.PruneProducer(ctx, time.Hour, 10)
	if err != nil {
		t.Fatalf("prune producer activations: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("pruned = %d, want 2 (completed-old and obsolete-old)", pruned)
	}
	stats, err = obligations.StatsProducer(ctx)
	if err != nil {
		t.Fatalf("producer activation stats after prune: %v", err)
	}
	if stats.ByState[activation.ProducerStateCompleted] != 1 ||
		stats.ByState[activation.ProducerStateObsolete] != 0 ||
		stats.ByState[activation.ProducerStateInapplicable] != 1 {
		t.Fatalf("stats after prune = %v, want completed 1, obsolete 0, inapplicable 1", stats.ByState)
	}
	pruned, err = obligations.PruneProducer(ctx, time.Hour, 10)
	if err != nil || pruned != 0 {
		t.Fatalf("second prune = %d, err = %v, want 0 and nil", pruned, err)
	}
	if _, err := obligations.PruneProducer(ctx, time.Hour, 0); err == nil {
		t.Fatal("prune with limit 0 succeeds, want an error")
	}
}

// TestProducerActivationDriftReopenLive proves the drift arm: a Terraform
// producer generation carrying the drift ARN replays the succeeded drift
// item that completed before the obligation was owed, while an item that
// completed after the obligation (it already read the new state) stays
// succeeded, so readiness never pays twice.
func TestProducerActivationDriftReopenLive(t *testing.T) {
	database := openProducerActivationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "producer-projector", time.Minute)
	producer := scope.IngestionScope{
		ScopeID:       "tfstate:producer",
		SourceSystem:  "terraform_state",
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "terraform_state:s3:producer",
	}

	if err := store.CommitScopeGeneration(ctx, producer,
		testfixtures.CatalogGeneration("tfstate:producer", "tf-old", base),
		testfixtures.FactChannel(producerTerraformFacts("tfstate:producer", "tf-old", producerDriftARN, base))); err != nil {
		t.Fatalf("commit terraform old generation: %v", err)
	}
	claimAckQuiet(t, ctx, queue, "tfstate:producer", "tf-old")
	if _, err := postgres.SettleProducerActivations(ctx, database); err != nil {
		t.Fatalf("settle old producer activation: %v", err)
	}

	// The stale item completed before the new generation's obligation is
	// owed; the fresh item completes after the Ack below.
	seedDriftProducerConsumerScope(t, ctx, database)
	seedDriftProducerItems(t, ctx, database, map[string]time.Time{"drift-stale/aws_cloud_runtime_drift": base})

	if err := store.CommitScopeGeneration(ctx, producer,
		testfixtures.CatalogGeneration("tfstate:producer", "tf-new", base.Add(time.Minute)),
		testfixtures.FactChannel(producerTerraformFacts("tfstate:producer", "tf-new", producerDriftARN, base.Add(time.Minute)))); err != nil {
		t.Fatalf("commit terraform new generation: %v", err)
	}
	claimAckQuiet(t, ctx, queue, "tfstate:producer", "tf-new")
	seedDriftProducerItems(t, ctx, database, map[string]time.Time{"drift-fresh/aws_cloud_runtime_drift": time.Now().UTC()})

	reopened, err := postgres.SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle producer activations: %v", err)
	}
	if len(reopened) != 1 || reopened["aws_cloud_runtime_drift"] != 1 {
		t.Fatalf("settle reopened %v, want exactly one aws_cloud_runtime_drift item", reopened)
	}
	if got := producerWorkItemStatus(t, ctx, database, "drift-stale/aws_cloud_runtime_drift"); got != "pending" {
		t.Fatalf("stale drift item status = %q, want pending", got)
	}
	if got := producerWorkItemStatus(t, ctx, database, "drift-fresh/aws_cloud_runtime_drift"); got != "succeeded" {
		t.Fatalf("fresh drift item status = %q, want succeeded", got)
	}
	if state, _ := producerObligationState(t, ctx, database, "tfstate:producer", "tf-new"); state != "completed" {
		t.Fatalf("obligation state = %q, want completed", state)
	}
}

// TestProducerActivationRetentionCascadeLive pins the new table's ON DELETE
// CASCADE: deleting a generation removes its producer obligation row. The
// default-off story depends on it (pending obligations wait for retention,
// not for a consumer), and a one-keyword DDL typo would otherwise break
// generation retention fleet-wide. Mirror of
// TestActivationObligationRetentionCascadeLive for activation_obligations.
func TestProducerActivationRetentionCascadeLive(t *testing.T) {
	database := openProducerActivationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	for _, step := range []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		VALUES ('t:cascade', 'container_registry_repository', 'oci_registry', 'cascade', 'oci_registry', 'cascade',
		    $1, $1, 'active', 'gen-cascade-active')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ('gen-cascade-active', 't:cascade', 'snapshot', $1, $1, 'active', $1),
		       ('gen-cascade-old', 't:cascade', 'snapshot', $1, $1, 'superseded', $1)`,
	} {
		if _, err := database.ExecContext(ctx, step, now); err != nil {
			t.Fatalf("seed cascade scope: %v", err)
		}
	}
	executor := postgres.SQLDB{DB: database}
	for _, generation := range []string{"gen-cascade-active", "gen-cascade-old"} {
		if err := activation.InsertProducerActivation(ctx, executor, "t:cascade", generation, "cascade-"+generation); err != nil {
			t.Fatalf("insert producer obligation %s: %v", generation, err)
		}
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM scope_generations WHERE generation_id = 'gen-cascade-old'`); err != nil {
		t.Fatalf("delete superseded generation: %v", err)
	}
	var remaining []string
	rows, err := database.QueryContext(ctx, `SELECT generation_id FROM producer_activation_obligations ORDER BY generation_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(remaining, []string{"gen-cascade-active"}) {
		t.Fatalf("remaining obligations = %v, want only gen-cascade-active", remaining)
	}
}

// TestProducerActivationInsertConflictBranchesLive pins the three
// insertProducerObligationQuery conflict behaviors: an obsolete row is owed
// again (back to pending), while completed and inapplicable rows stay
// terminal. Normal duplicate Acks never reach these branches (ackRows != 1
// rejects them first), so a WHERE typo would otherwise silently revive a
// terminal obligation and violate exactly-once if recovery or an admin
// re-Ack ever hit it.
func TestProducerActivationInsertConflictBranchesLive(t *testing.T) {
	database := openProducerActivationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	for _, step := range []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		VALUES ('t:conflict', 'container_registry_repository', 'oci_registry', 'conflict', 'oci_registry', 'conflict',
		    $1, $1, 'active', 'gen-done')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ('gen-done', 't:conflict', 'snapshot', $1, $1, 'active', $1),
		       ('gen-stale', 't:conflict', 'snapshot', $1, $1, 'superseded', $1),
		       ('gen-empty', 't:conflict', 'snapshot', $1, $1, 'superseded', $1)`,
		`INSERT INTO producer_activation_obligations
		    (scope_id, generation_id, state, work_item_id, created_at, finished_at)
		VALUES ('t:conflict', 'gen-done', 'completed', 'conflict-gen-done', $1, $1),
		       ('t:conflict', 'gen-stale', 'obsolete', 'conflict-gen-stale', $1, $1),
		       ('t:conflict', 'gen-empty', 'inapplicable', 'conflict-gen-empty', $1, $1)`,
	} {
		if _, err := database.ExecContext(ctx, step, now); err != nil {
			t.Fatalf("seed conflict scope: %v", err)
		}
	}
	executor := postgres.SQLDB{DB: database}
	for _, generation := range []string{"gen-done", "gen-stale", "gen-empty"} {
		if err := activation.InsertProducerActivation(ctx, executor, "t:conflict", generation, "reack-"+generation); err != nil {
			t.Fatalf("re-insert producer obligation %s: %v", generation, err)
		}
	}
	for generation, want := range map[string]string{
		"gen-done": "completed", "gen-stale": "pending", "gen-empty": "inapplicable",
	} {
		var (
			state    string
			finished sql.NullTime
		)
		if err := database.QueryRowContext(ctx,
			`SELECT state, finished_at FROM producer_activation_obligations WHERE scope_id = 't:conflict' AND generation_id = $1`,
			generation).Scan(&state, &finished); err != nil {
			t.Fatalf("read producer obligation %s: %v", generation, err)
		}
		if state != want {
			t.Errorf("obligation %s state = %q, want %q", generation, state, want)
		}
		if want == "pending" && finished.Valid {
			t.Errorf("re-owed obligation %s keeps finished_at %v, want NULL", generation, finished.Time)
		}
		if want != "pending" && !finished.Valid {
			t.Errorf("terminal obligation %s lost finished_at, want it kept", generation)
		}
	}
}
