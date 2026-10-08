// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestProducerActivationRunnerStoreSettlesThroughThePort drives the consumer
// storage port end to end on the cost corpus: claim, settle with the real
// outcome, census, and prune.
func TestProducerActivationRunnerStoreSettlesThroughThePort(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, dsnForDeferredPartitionMemoProof(t), "producer_port")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerDependentCostCorpus(t, ctx, database)

	port := ProducerActivationRunnerStore{
		Activation: activation.NewStore(SQLDB{DB: database}),
		Ingestion:  NewIngestionStore(SQLDB{DB: database}),
	}
	work, err := port.ClaimProducerActivation(ctx, "port-owner", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("claim producer activation: claimed=%v err=%v", work != nil, err)
	}
	result, err := port.SettleProducerActivation(ctx, *work)
	if err != nil {
		t.Fatalf("settle producer activation: %v", err)
	}
	if result.Outcome != maintenance.ProducerActivationOutcomeCompleted {
		t.Fatalf("settle outcome = %q, want completed", result.Outcome)
	}
	if len(result.Reopened) != 1 || result.Reopened["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("settle reopened %v, want one kubernetes_correlation_materialization item", result.Reopened)
	}
	again, err := port.ClaimProducerActivation(ctx, "port-owner", time.Minute)
	if err != nil || again != nil {
		t.Fatalf("second claim: claimed=%v err=%v, want nothing claimable", again != nil, err)
	}
	stats, err := port.ProducerActivationStats(ctx)
	if err != nil {
		t.Fatalf("producer activation stats: %v", err)
	}
	if stats.ByState["completed"] != 1 || stats.ByState["pending"] != 0 {
		t.Fatalf("stats by state = %v, want one completed and no pending", stats.ByState)
	}
	pruned, err := port.PruneProducerActivations(ctx, 0, 10)
	if err != nil {
		t.Fatalf("prune producer activations: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
}

// TestProducerSettleErrorMapsPortSentinels pins the adapter's error mapping:
// a lease lost inside the transaction, a lock timeout, and everything else
// passing through unchanged.
func TestProducerSettleErrorMapsPortSentinels(t *testing.T) {
	t.Parallel()
	leaseLost := fmt.Errorf("settle claimed producer activation: reopen: %w", ErrProducerLeaseLost)
	if err := producerSettleError(leaseLost); !errors.Is(err, maintenance.ErrProducerActivationLeaseLost) {
		t.Fatalf("lease lost maps to %v, want ErrProducerActivationLeaseLost", err)
	}
	lockTimeout := fmt.Errorf("settle claimed producer activation: %w", &pgconn.PgError{Code: "55P03"})
	if err := producerSettleError(lockTimeout); !errors.Is(err, maintenance.ErrProducerActivationSettleLockTimeout) {
		t.Fatalf("lock timeout maps to %v, want ErrProducerActivationSettleLockTimeout", err)
	}
	other := errors.New("connection reset")
	if err := producerSettleError(other); !errors.Is(err, other) || errors.Is(err, maintenance.ErrProducerActivationLeaseLost) {
		t.Fatalf("other error maps to %v, want it unchanged", err)
	}
	if err := producerSettleError(nil); err != nil {
		t.Fatalf("nil maps to %v, want nil", err)
	}
}

// TestProducerListingsDeriveFromShipped pins that both #7635 dependency-index
// listings are the shipped correlation reopen listing plus exactly the
// linkage conjunct, never a hand-copied second query. The shipped text before
// the marker must be a byte prefix of the derived query, the shipped text
// after the marker its byte suffix, and the only bytes between them the
// inserted conjunct. A drift between the shipped floor and the settle
// listings would resurrect the superseded-generation churn #7637 removed.
func TestProducerListingsDeriveFromShipped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		derived  string
		conjunct string
	}{
		{
			name:     "OCI dependents bounded by key linkage",
			derived:  listProducerDependentOCIItemsQuery,
			conjunct: producerOCILinkageConjunct,
		},
		{
			name:     "drift dependents bounded by ARN linkage",
			derived:  listProducerDependentDriftItemsQuery,
			conjunct: producerDriftLinkageConjunct,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Count(listSucceededReducerWorkItemsByDomainQuery, correlationReopenStageMarker); got != 1 {
				t.Fatalf("shipped query holds marker %q %d times, want 1",
					correlationReopenStageMarker, got)
			}
			cut := strings.Index(listSucceededReducerWorkItemsByDomainQuery, correlationReopenStageMarker) +
				len(correlationReopenStageMarker)
			prefix, suffix := listSucceededReducerWorkItemsByDomainQuery[:cut], listSucceededReducerWorkItemsByDomainQuery[cut:]
			if !strings.HasPrefix(tc.derived, prefix) {
				t.Fatal("derived query does not start with the shipped prefix")
			}
			if !strings.HasSuffix(tc.derived, suffix) {
				t.Fatal("derived query does not end with the shipped suffix")
			}
			if middle := tc.derived[len(prefix) : len(tc.derived)-len(suffix)]; middle != tc.conjunct {
				t.Fatalf("derived query inserts %q, want exactly %q", middle, tc.conjunct)
			}
		})
	}
}

// TestProducerImageRefParseParity pins the SQL image-reference mirror against
// ParseContainerImageRef over a corpus of realistic references: digest form,
// tag form, registries with ports, case folding, surrounding spaces, and the
// unparseable shapes. A NULL SQL component equals Go's empty string (both
// mean "absent"); an all-absent row must agree with Go's ok=false. The probe
// is a read-only SELECT needing no schema, so it runs on the bare proof DSN.
//
// Known corner (documented in ingestion_producer_activation_sql.go): Go
// strings.TrimSpace strips unicode whitespace while SQL btrim strips spaces
// only, so references carrying tabs or newlines are out of scope here.
func TestProducerImageRefParseParity(t *testing.T) {
	dsn := postgresproof.DeferredPartitionProofDSN(t)
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	corpus := []string{
		"example/app:1.2.3",
		"example/app@sha256:abc123",
		"registry.example.com:5000/team/app:v1",
		"Registry.Example.COM/Team/App:LATEST",
		"  example/app:v1  ",
		"example/app",
		"example/app:",
		"",
		"example/app@md5:abc",
		"app:v1",
		"example/app@sha256:abc:def",
		"@sha256:abc",
		"example/app:v1@sha256:abc",
		"/example/app//:v2",
		"localhost:5000/app",
	}
	for i, ref := range corpus {
		t.Run(fmt.Sprintf("ref_%02d", i), func(t *testing.T) {
			want, wantOK := containerimage.ParseContainerImageRef(ref)
			var repo, tag, digest sql.NullString
			if err := database.QueryRowContext(ctx, producerParseRefProbeSQL, ref).
				Scan(&repo, &tag, &digest); err != nil {
				t.Fatalf("parse probe %q: %v", ref, err)
			}
			got := map[string]string{"repo": repo.String, "tag": tag.String, "digest": digest.String}
			wantMap := map[string]string{"repo": "", "tag": "", "digest": ""}
			if wantOK {
				wantMap["repo"], wantMap["tag"], wantMap["digest"] = want.RepositoryKey, want.Tag, want.Digest
			}
			if !reflect.DeepEqual(got, wantMap) {
				t.Fatalf("parse probe %q = %v, ParseContainerImageRef = %v (ok=%v)",
					ref, got, wantMap, wantOK)
			}
		})
	}
}

// consumersForProducerDependentCostProof sizes the corpus the settle cost
// proof measures: one owed producer generation against hundreds of floored
// succeeded consumers, of which exactly one links to the owed keys.
const consumersForProducerDependentCostProof = 300

// TestProducerDependentListingCostLive measures the #7635 settle's
// selectivity bound: against hundreds of floored succeeded consumers the OCI
// listing returns exactly the one linked item, and the production settle
// reopens exactly that item and completes. The bound is a row count, stable
// across hosts; the EXPLAIN (ANALYZE, BUFFERS) plan is logged for the
// evidence note, not asserted, because plan shape is statistics-bound.
func TestProducerDependentListingCostLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, dsnForDeferredPartitionMemoProof(t), "producer_cost")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerDependentCostCorpus(t, ctx, database)

	owedAt := time.Now().UTC()
	owedKeys, err := listProducerOwedOCIKeys(ctx, SQLDB{DB: database}, "oci:producer", "producer-new")
	if err != nil {
		t.Fatalf("list producer owed OCI keys: %v", err)
	}
	ids, err := listProducerDependentItemIDs(ctx, SQLDB{DB: database}, listProducerDependentOCIItemsQuery,
		"kubernetes_correlation_materialization", owedAt, owedKeys.digests, owedKeys.pairRepos, owedKeys.pairTags)
	if err != nil {
		t.Fatalf("list producer dependent OCI items: %v", err)
	}
	if len(ids) != 1 || ids[0] != "cost-linked/kubernetes_correlation_materialization" {
		t.Fatalf("dependent listing = %v, want exactly [cost-linked/kubernetes_correlation_materialization]", ids)
	}

	planRows, err := database.QueryContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS) "+listProducerDependentOCIItemsQuery,
		"kubernetes_correlation_materialization", owedAt, owedKeys.digests, owedKeys.pairRepos, owedKeys.pairTags)
	if err != nil {
		t.Fatalf("explain dependent listing: %v", err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	_ = planRows.Close()
	if err := planRows.Err(); err != nil {
		t.Fatalf("explain dependent listing rows: %v", err)
	}
	t.Logf("dependent OCI listing plan (300 disjoint + 1 linked consumers):\n%s", plan.String())

	start := time.Now()
	counts, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle producer activations: %v", err)
	}
	t.Logf("settle wall = %s (host-bound, informational only)", time.Since(start).Round(time.Millisecond))
	if len(counts) != 1 || counts["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("settle reopened %v, want exactly one kubernetes_correlation_materialization item", counts)
	}
	again, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("second settle: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second settle reopened %v, want nothing", again)
	}
}

// seedProducerDependentCostCorpus seeds one owed OCI producer generation with
// a pending obligation plus 300 disjoint and 1 linked k8s consumers, all
// floored and succeeded with completion times before the obligation.
func seedProducerDependentCostCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const digestOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const digestNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	statements := []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('oci:producer', 'container_registry_repository', 'oci_registry', 'example/app',
		         'oci_registry', 'example/app',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'producer-new')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('producer-old', 'oci:producer', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('producer-new', 'oci:producer', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 01:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('cost-manifest', 'oci:producer', 'producer-new', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:cost-manifest', 'oci_registry', 'cost-manifest',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', FALSE,
		         '{"repository_id": "example/app", "digest": "%s"}'::jsonb),
		        ('cost-tag', 'oci:producer', 'producer-new', 'oci_registry.image_tag_observation',
		         'oci_registry.image_tag_observation:cost-tag', 'oci_registry', 'cost-tag',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', FALSE,
		         '{"repository_id": "example/app", "tag": "v1", "resolved_digest": "%s",
		           "previous_digest": "%s"}'::jsonb)`, digestNew, digestNew, digestOld),
		`INSERT INTO producer_activation_obligations (scope_id, generation_id, state, work_item_id, created_at)
		 VALUES ('oci:producer', 'producer-new', 'pending', 'cost-obligation', clock_timestamp())`,
		fmt.Sprintf(`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 SELECT 'k8s:cost-' || s, 'cluster', 'kubernetes', 'cost-' || s, 'kubernetes_live', 'cost-' || s,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		        'active', 'cost-' || s || '-current'
		 FROM generate_series(1, %d) AS s`, consumersForProducerDependentCostProof),
		fmt.Sprintf(`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 SELECT 'cost-' || s || '-current', 'k8s:cost-' || s, 'sync',
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		        'active', TIMESTAMPTZ '2026-07-01 00:00:00+00'
		 FROM generate_series(1, %d) AS s`, consumersForProducerDependentCostProof),
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT 'cost-pod-' || s, 'k8s:cost-' || s, 'cost-' || s || '-current',
		        'kubernetes_live.pod_template', 'kubernetes_live.pod_template:cost-pod-' || s,
		        'kubernetes', 'cost-pod-' || s,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		        jsonb_build_object('object_id', 'pod-cost-' || s,
		          'image_refs', jsonb_build_array('other/cost-' || s || ':v9'),
		          'containers', jsonb_build_array(jsonb_build_object('name', 'app',
		            'image', 'other/cost-' || s || ':v9',
		            'resolved_image_digest', 'other/cost-' || s || '@sha256:9999' || s)))
		 FROM generate_series(1, %d) AS s`, consumersForProducerDependentCostProof),
		fmt.Sprintf(`INSERT INTO fact_work_items
		    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
		 SELECT 'cost-' || s || '-current/kubernetes_correlation_materialization',
		        'k8s:cost-' || s, 'cost-' || s || '-current',
		        'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00'
		 FROM generate_series(1, %d) AS s`, consumersForProducerDependentCostProof),
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('k8s:cost-linked', 'cluster', 'kubernetes', 'cost-linked', 'kubernetes_live', 'cost-linked',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'cost-linked')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('cost-linked', 'k8s:cost-linked', 'sync',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('cost-linked-pod', 'k8s:cost-linked', 'cost-linked', 'kubernetes_live.pod_template',
		         'kubernetes_live.pod_template:cost-linked-pod', 'kubernetes', 'cost-linked-pod',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		         '{"object_id": "pod-cost-linked", "image_refs": ["example/app:v1"],
		           "containers": [{"name": "app", "image": "example/app:v1",
		             "resolved_image_digest": "example/app@%s"}]}'::jsonb)`, digestOld),
		`INSERT INTO fact_work_items
		    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
		 VALUES ('cost-linked/kubernetes_correlation_materialization', 'k8s:cost-linked',
		        'cost-linked', 'reducer', 'kubernetes_correlation_materialization', 'succeeded', 1,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00')`,
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer dependent cost corpus: %v", err)
		}
	}
}
