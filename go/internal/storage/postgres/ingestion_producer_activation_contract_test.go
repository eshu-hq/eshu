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
	"regexp"
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

// TestCommittedSettleCountsEmptyUnlessCompleted pins the reopen-count
// contract: only a completed settle committed its reopen, so only it
// reports nonzero counts. A lost lease rolls back; the not_owner outcome
// reports empty counts and the retry counts the rows (#7635 review F1).
func TestCommittedSettleCountsEmptyUnlessCompleted(t *testing.T) {
	t.Parallel()
	counts := map[string]int{"kubernetes_correlation_materialization": 2}
	if got := committedSettleCounts(counts, activation.ProducerCompleted); !reflect.DeepEqual(got, counts) {
		t.Fatalf("completed counts = %v, want %v", got, counts)
	}
	for _, outcome := range []activation.ProducerOutcome{
		activation.ProducerNotOwner,
		activation.ProducerObsolete,
		activation.ProducerInapplicable,
		activation.ProducerMissing,
	} {
		if got := committedSettleCounts(counts, outcome); len(got) != 0 {
			t.Fatalf("%s counts = %v, want empty", outcome, got)
		}
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

// fileHeavyProducerProbeFacts is the representative file-heavy generation
// for the Ack probe cost proof: enough parsed file facts to expose a full
// generation scan, with payloads shaped like real parsed files.
const fileHeavyProducerProbeFacts = 2000

// TestProducerEvidenceProbeCostLive is the F1 settle-probe cost proof: it runs
// the exact producerEvidenceExistsQuery the producer settle executes per
// obligation against representative generations (file-heavy with no producer
// evidence, non-producer noise, OCI producer, drift producer, empty,
// all-arms) and logs the EXPLAIN (ANALYZE, BUFFERS) plan for each. The test
// asserts the probe outcome per generation; the plans and buffer counts are
// the evidence the README's No-Regression note cites, not gated assertions.
// The probe deliberately runs in the settle, not in Ack: it plans in ~8.4 ms
// per call through the Go driver, which belongs on the background runner.
func TestProducerEvidenceProbeCostLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, dsnForDeferredPartitionMemoProof(t), "producer_probe_cost")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerEvidenceProbeCorpus(t, ctx, database)

	for _, generation := range []struct {
		id   string
		want bool
	}{
		{"probe-file-heavy", false},
		{"probe-noise", false},
		{"probe-producer", true},
		{"probe-drift", true},
		{"probe-empty", false},
		{"probe-all-arms", true},
	} {
		got, err := GenerationCarriesProducerEvidence(ctx, SQLDB{DB: database}, "git:probe", generation.id)
		if err != nil {
			t.Fatalf("probe producer evidence %s: %v", generation.id, err)
		}
		if got != generation.want {
			t.Fatalf("probe producer evidence %s = %v, want %v", generation.id, got, generation.want)
		}
		planRows, err := database.QueryContext(ctx,
			"EXPLAIN (ANALYZE, BUFFERS) "+producerEvidenceExistsQuery, "git:probe", generation.id)
		if err != nil {
			t.Fatalf("explain producer probe %s: %v", generation.id, err)
		}
		var plan strings.Builder
		for planRows.Next() {
			var line string
			if err := planRows.Scan(&line); err != nil {
				t.Fatalf("scan explain row %s: %v", generation.id, err)
			}
			plan.WriteString(line + "\n")
		}
		_ = planRows.Close()
		if err := planRows.Err(); err != nil {
			t.Fatalf("explain producer probe %s rows: %v", generation.id, err)
		}
		t.Logf("producer probe plan %s:\n%s", generation.id, plan.String())
	}
}

// TestProducerEvidenceKindPrefilterDifferential proves the probe's kind
// prefilter never changes the outcome: the shipped prefiltered query and
// the unprefiltered query agree on every corpus generation, including one
// carrying every identity-filter arm kind. The all-arms generation masks a
// single-arm derivation miss (other matching rows still agree), so the
// exact derived list is pinned separately by
// TestProducerEvidenceKindDerivation; only the isolated kinds
// (image_manifest, terraform_state_resource) would disagree here.
func TestProducerEvidenceKindPrefilterDifferential(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, dsnForDeferredPartitionMemoProof(t), "producer_probe_diff")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerEvidenceProbeCorpus(t, ctx, database)

	run := func(query, generation string) bool {
		t.Helper()
		var exists bool
		if err := database.QueryRowContext(ctx, query, "git:probe", generation).Scan(&exists); err != nil {
			t.Fatalf("run probe variant on %s: %v", generation, err)
		}
		return exists
	}
	for _, generation := range []string{
		"probe-file-heavy", "probe-noise", "probe-producer", "probe-drift", "probe-empty", "probe-all-arms",
		"probe-bulk-1", "probe-bulk-10",
	} {
		filtered := run(producerEvidenceExistsQuery, generation)
		unfiltered := run(producerEvidenceExistsUnprefilteredQuery, generation)
		if filtered != unfiltered {
			t.Errorf("probe disagreement on %s: prefiltered=%v unprefiltered=%v", generation, filtered, unfiltered)
		}
	}
}

// TestProducerEvidenceKindDerivation pins the derived kind list: every kind
// the identity filter matches, in filter order, plus the drift arm's kind
// when derived from the composed core predicate (the prefilter derives
// from the core text itself, never a hand-copied literal).
func TestProducerEvidenceKindDerivation(t *testing.T) {
	want := []string{
		"oci_registry.image_tag_observation", "oci_registry.image_manifest", "oci_registry.image_index",
		"aws_image_reference", "azure_image_reference", "gcp_image_reference",
		"aws_relationship", "content_entity", "file",
	}
	if got := producerEvidenceFactKindsFromFilter(identityFactFilterSQL); !reflect.DeepEqual(got, want) {
		t.Fatalf("derived filter kinds = %v, want %v", got, want)
	}
	wantCore := append(append([]string{}, want...), "terraform_state_resource")
	if got := producerEvidenceFactKindsFromFilter(producerEvidenceCoreSQL); !reflect.DeepEqual(got, wantCore) {
		t.Fatalf("derived core kinds = %v, want %v", got, wantCore)
	}
}

// TestProducerEvidenceKindDerivationCoversEveryMention fails closed on
// future filter arms. Every fact_kind mention in the identity filter and
// in the composed core predicate (which adds the drift arm) must satisfy
// three guards: it is consumed by the derivation pattern (so a LIKE or
// unparseable arm breaks the test), its operator is a positive IN or
// equality (so != or <> cannot hide behind the = inside it), and every
// derived kind is a plain literal (so a subquery or nested IN cannot yield
// garbage that silently shrinks the prefilter and retires a producer
// generation inapplicable, #7708 review).
// producerDerivedKindLiteralPattern accepts only plain fact_kind
// literals: letters, digits, dots, underscores. A derived kind outside
// this shape is regex garbage from an arm the derivation cannot express
// (subquery or nested IN), not a kind the prefilter may list.
var producerDerivedKindLiteralPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

func TestProducerEvidenceKindDerivationCoversEveryMention(t *testing.T) {
	t.Parallel()
	if err := checkFilterDerivationCoversEveryMention(identityFactFilterSQL); err != nil {
		t.Fatal(err)
	}
	if err := checkFilterDerivationCoversEveryMention(producerEvidenceCoreSQL); err != nil {
		t.Fatal(err)
	}
}

// TestCheckFilterDerivationCoversEveryMention pins the fail-closed guards:
// each hostile arm shape a future identity-filter edit could introduce
// must trip the checker, never silently shrink the prefilter.
func TestCheckFilterDerivationCoversEveryMention(t *testing.T) {
	t.Parallel()
	hostile := map[string]string{
		"like arm":          `(fact.fact_kind LIKE 'oci_registry.%')`,
		"negated equality":  `(fact.fact_kind != 'file')`,
		"not-prefixed arm":  `(NOT fact.fact_kind = 'file')`,
		"subquery IN":       `(fact.fact_kind IN (SELECT kind FROM producer_kinds))`,
		"nested IN":         `(fact.fact_kind IN ('file', ('content_entity')))`,
		"double-quoted arm": `(fact.fact_kind = "file")`,
	}
	for name, filter := range hostile {
		if err := checkFilterDerivationCoversEveryMention(filter); err == nil {
			t.Errorf("%s: checker accepted an arm the derivation cannot express", name)
		}
	}
}

// checkFilterDerivationCoversEveryMention reports whether every fact_kind
// mention in filter is safely consumed by the derivation: covered by the
// pattern, positively operated, unnegated, and yielding plain literals.
func checkFilterDerivationCoversEveryMention(filter string) error {
	matches := producerEvidenceFactKindPattern.FindAllStringIndex(filter, -1)
	if len(matches) == 0 {
		return errors.New("derivation pattern matched nothing in the identity filter")
	}
	covered := func(at int) bool {
		for _, span := range matches {
			if at >= span[0] && at < span[1] {
				return true
			}
		}
		return false
	}
	rest, base := filter, 0
	for {
		at := strings.Index(rest, "fact_kind")
		if at < 0 {
			break
		}
		pos := base + at
		context := rest[at:]
		if len(context) > 60 {
			context = context[:60]
		}
		if !covered(pos) {
			return fmt.Errorf("fact_kind mention at offset %d is outside the derivation pattern: %q", pos, context)
		}
		after := rest[at+len("fact_kind"):]
		trimmed := strings.TrimLeft(after, " \t\n")
		positive := strings.HasPrefix(trimmed, "=") ||
			(strings.HasPrefix(trimmed, "IN") && len(trimmed) > 2 &&
				(trimmed[2] == '(' || trimmed[2] == ' ' || trimmed[2] == '\t' || trimmed[2] == '\n'))
		if !positive {
			return fmt.Errorf("fact_kind mention at offset %d uses an operator the derivation cannot express positively: %q", pos, context)
		}
		before := strings.TrimRight(strings.TrimSuffix(filter[:pos], "fact."), " \t\n")
		if strings.HasSuffix(before, "NOT") || strings.HasSuffix(before, "!") ||
			strings.HasSuffix(before, "<") || strings.HasSuffix(before, ">") {
			return fmt.Errorf("fact_kind mention at offset %d is negated, which the derivation cannot express: %q", pos, context)
		}
		rest, base = after, pos+len("fact_kind")
	}
	for _, kind := range producerEvidenceFactKindsFromFilter(filter) {
		if !producerDerivedKindLiteralPattern.MatchString(kind) {
			return fmt.Errorf("derived kind %q is not a plain literal; the filter arm needs a derivation the regex cannot express", kind)
		}
	}
	return nil
}

// seedProducerEvidenceProbeCorpus builds the settle-probe cost generations: a
// file-heavy generation with parsed file facts that carry no producer
// evidence (the worst case: the probe must rule out every arm), a noise
// generation with non-producer facts the kind prefilter skips, an OCI
// producer, a drift producer, an empty generation, one generation carrying
// every identity-filter arm kind, and bulk OCI generations that make the
// corpus large enough to expose a missing generation anchor.
func seedProducerEvidenceProbeCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes
		    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		     observed_at, ingested_at, status, active_generation_id)
		 VALUES ('git:probe', 'repository', 'git', 'probe', 'git', 'probe',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', 'probe-file-heavy')`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 SELECT 'probe-bulk-' || s, 'git:probe', 'snapshot',
		        TIMESTAMPTZ '2026-06-01 00:00:00+00', TIMESTAMPTZ '2026-06-01 00:00:00+00',
		        'superseded', TIMESTAMPTZ '2026-06-01 00:00:00+00'
		 FROM generate_series(1, 10) AS s`,
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT 'probe-bulk-' || s, 'git:probe', 'probe-bulk-' || ((s - 1) / 10000 + 1),
		        'oci_registry.image_manifest',
		        'oci_registry.image_manifest:probe-bulk-' || s, 'oci_registry', 'probe-bulk-' || s,
		        TIMESTAMPTZ '2026-06-01 00:00:00+00', TIMESTAMPTZ '2026-06-01 00:00:00+00', FALSE,
		        '{"repository_id": "example/bulk", "digest": "sha256:4444444444444444444444444444444444444444444444444444444444444444"}'::jsonb
		 FROM generate_series(1, 100000) AS s`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('probe-file-heavy', 'git:probe', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00',
		         'active', TIMESTAMPTZ '2026-07-01 00:00:00+00'),
		        ('probe-producer', 'git:probe', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 01:00:00+00'),
		        ('probe-drift', 'git:probe', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 02:00:00+00', TIMESTAMPTZ '2026-07-01 02:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 02:00:00+00'),
		        ('probe-empty', 'git:probe', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 03:00:00+00', TIMESTAMPTZ '2026-07-01 03:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 03:00:00+00')`,
		fmt.Sprintf(`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT 'probe-file-' || s, 'git:probe', 'probe-file-heavy', 'file',
		        'file:probe-file-' || s, 'git', 'probe-file-' || s,
		        TIMESTAMPTZ '2026-07-01 00:00:00+00', TIMESTAMPTZ '2026-07-01 00:00:00+00', FALSE,
		        jsonb_build_object('path', 'src/file-' || s || '.go',
		            'parsed_file_data', jsonb_build_object('symbols',
		                (SELECT jsonb_agg(jsonb_build_object('name', 'Sym' || n, 'kind', 'func'))
		                 FROM generate_series(1, 20) AS n)))
		 FROM generate_series(1, %d) AS s`, fileHeavyProducerProbeFacts),
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('probe-manifest', 'git:probe', 'probe-producer', 'oci_registry.image_manifest',
		         'oci_registry.image_manifest:probe-manifest', 'oci_registry', 'probe-manifest',
		         TIMESTAMPTZ '2026-07-01 01:00:00+00', TIMESTAMPTZ '2026-07-01 01:00:00+00', FALSE,
		         '{"repository_id": "example/app", "digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333"}'::jsonb)`,
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ('probe-state', 'git:probe', 'probe-drift', 'terraform_state_resource',
		         'terraform_state_resource:probe-state', 'terraform', 'probe-state',
		         TIMESTAMPTZ '2026-07-01 02:00:00+00', TIMESTAMPTZ '2026-07-01 02:00:00+00', FALSE,
		         '{"attributes": {"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-probe"}}'::jsonb)`,
		`INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 VALUES ('probe-noise', 'git:probe', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 05:00:00+00', TIMESTAMPTZ '2026-07-01 05:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 05:00:00+00'),
		        ('probe-all-arms', 'git:probe', 'snapshot',
		         TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00',
		         'superseded', TIMESTAMPTZ '2026-07-01 04:00:00+00')`,
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT 'probe-noise-' || s, 'git:probe', 'probe-noise', 'code_symbol',
		        'code_symbol:probe-noise-' || s, 'git', 'probe-noise-' || s,
		        TIMESTAMPTZ '2026-07-01 05:00:00+00', TIMESTAMPTZ '2026-07-01 05:00:00+00', FALSE,
		        '{"name": "NoiseSymbol"}'::jsonb
		 FROM generate_series(1, 5000) AS s`,
		`INSERT INTO fact_records
		    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		     source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES
		  ('probe-arm-tag', 'git:probe', 'probe-all-arms', 'oci_registry.image_tag_observation',
		   'oci_registry.image_tag_observation:probe-arm-tag', 'oci_registry', 'probe-arm-tag',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE, '{}'::jsonb),
		  ('probe-arm-manifest', 'git:probe', 'probe-all-arms', 'oci_registry.image_manifest',
		   'oci_registry.image_manifest:probe-arm-manifest', 'oci_registry', 'probe-arm-manifest',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE, '{}'::jsonb),
		  ('probe-arm-index', 'git:probe', 'probe-all-arms', 'oci_registry.image_index',
		   'oci_registry.image_index:probe-arm-index', 'oci_registry', 'probe-arm-index',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE, '{}'::jsonb),
		  ('probe-arm-aws', 'git:probe', 'probe-all-arms', 'aws_image_reference',
		   'aws_image_reference:probe-arm-aws', 'aws', 'probe-arm-aws',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE, '{}'::jsonb),
		  ('probe-arm-azure', 'git:probe', 'probe-all-arms', 'azure_image_reference',
		   'azure_image_reference:probe-arm-azure', 'azure', 'probe-arm-azure',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE, '{}'::jsonb),
		  ('probe-arm-gcp', 'git:probe', 'probe-all-arms', 'gcp_image_reference',
		   'gcp_image_reference:probe-arm-gcp', 'gcp', 'probe-arm-gcp',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE, '{}'::jsonb),
		  ('probe-arm-rel', 'git:probe', 'probe-all-arms', 'aws_relationship',
		   'aws_relationship:probe-arm-rel', 'aws', 'probe-arm-rel',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE,
		   '{"target_type": "container_image"}'::jsonb),
		  ('probe-arm-entity', 'git:probe', 'probe-all-arms', 'content_entity',
		   'content_entity:probe-arm-entity', 'git', 'probe-arm-entity',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE,
		   '{"entity_metadata": {"container_images": []}}'::jsonb),
		  ('probe-arm-file', 'git:probe', 'probe-all-arms', 'file',
		   'file:probe-arm-file', 'git', 'probe-arm-file',
		   TIMESTAMPTZ '2026-07-01 04:00:00+00', TIMESTAMPTZ '2026-07-01 04:00:00+00', FALSE,
		   '{"parsed_file_data": {"dockerfile_stages": []}}'::jsonb)`,
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed producer evidence probe corpus: %v", err)
		}
	}
}
