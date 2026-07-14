// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const (
	relationshipFamilyFullBackfillProofDSNEnv      = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_PROOF_DSN"
	relationshipFamilyFullBackfillProofConfirmEnv  = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_PROOF_CONFIRM"
	relationshipFamilyFullBackfillProofModeEnv     = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_PROOF_MODE"
	relationshipFamilyFullBackfillExpectedFactsEnv = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_FACTS"
	relationshipFamilyFullBackfillExpectedRefsEnv  = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_REFS"
	relationshipFamilyFullBackfillExpectedPartsEnv = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_PARTITIONS"
	relationshipFamilyFullBackfillExpectedLoadEnv  = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_LOADED"
	relationshipFamilyFullBackfillExpectedEvidEnv  = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_EVIDENCE"
	relationshipFamilyFullBackfillExpectedReadyEnv = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_READINESS"
	relationshipFamilyFullBackfillExpectedMemosEnv = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_MEMOS"
	relationshipFamilyFullBackfillExpectedQueueEnv = "ESHU_RELATIONSHIP_FAMILY_FULL_BACKFILL_EXPECTED_QUEUE_SUCCEEDED"
)

const relationshipFamilyBinaryProofIndexStateQuery = `
SELECT idx.indisvalid, idx.indisready, pg_relation_size(index_class.oid),
       pg_get_expr(idx.indpred, idx.indrelid)
FROM pg_class AS index_class
JOIN pg_index AS idx ON idx.indexrelid = index_class.oid
JOIN pg_class AS table_class ON table_class.oid = idx.indrelid
JOIN pg_namespace AS namespace ON namespace.oid = table_class.relnamespace
WHERE index_class.relname = $1
  AND namespace.nspname = current_schema()
  AND table_class.relname = 'fact_records'
`

type relationshipFamilyBinaryProofExpected struct {
	facts          int64
	references     int64
	partitions     int64
	loaded         int64
	evidence       int64
	readiness      int64
	memos          int64
	queueSucceeded int64
	maxDuration    time.Duration
	minReadCalls   int
	minReadCursors int
	minWriteTx     int
	minWriteCalls  int
}

type relationshipFamilyBinaryProofResult struct {
	elapsed time.Duration
	summary string
}

// TestRelationshipFamilyRetainedFullBackfillBinaryProof drives the exact
// production deferred-backfill method through a compiled test binary. Local
// mode seeds the retained-shape Odù in an isolated schema. Remote mode refuses
// every database except the two dedicated filtered retained-data proof databases.
func TestRelationshipFamilyRetainedFullBackfillBinaryProof(t *testing.T) {
	t.Setenv("ESHU_DEFERRED_BACKFILL_CONCURRENCY", "8")

	if dsn := strings.TrimSpace(os.Getenv(relationshipFamilyFullBackfillProofDSNEnv)); dsn != "" {
		runRelationshipFamilyRetainedBinaryProof(t, dsn)
		return
	}
	runRelationshipFamilyLocalBinaryProof(t)
}

func runRelationshipFamilyLocalBinaryProof(t *testing.T) {
	t.Helper()
	proof := openRelationshipFamilyProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for name, ddl := range map[string]string{
		"relationship evidence": relationshipSchemaSQL,
		"phase state":           graphProjectionPhaseStateSchemaSQL,
		"partition memo":        deferredBackfillPartitionMemoSchemaSQL,
		"empty work queue":      `CREATE TABLE fact_work_items (work_item_id TEXT PRIMARY KEY)`,
	} {
		if _, err := proof.scoped.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("create local %s schema: %v", name, err)
		}
	}

	odu, _, _ := seedRelationshipFamilyProofOdu(t, ctx, proof.scoped)
	partitions, err := loadActiveScopeGenerationPartitions(ctx, SQLDB{DB: proof.scoped})
	if err != nil {
		t.Fatalf("load local proof partitions: %v", err)
	}
	repositories, err := loadActiveRepositoryGenerations(ctx, SQLDB{DB: proof.scoped})
	if err != nil {
		t.Fatalf("load local proof repositories: %v", err)
	}
	expectedEvidence := relationships.DedupeEvidenceFacts(ifa.DiscoveredEvidence(odu))
	expected := relationshipFamilyBinaryProofExpected{
		facts:          int64(len(odu.Facts)),
		references:     countRelationshipFamilyBinaryProofRows(t, ctx, proof.scoped, "relationship_reference_candidate_keys"),
		partitions:     int64(len(partitions)),
		loaded:         84,
		evidence:       int64(len(expectedEvidence)),
		readiness:      int64(len(repositories)),
		memos:          11,
		maxDuration:    30 * time.Second,
		minReadCalls:   8,
		minReadCursors: 1,
		minWriteTx:     1,
		minWriteCalls:  1,
	}

	baseline := executeRelationshipFamilyBinaryProof(t, ctx, proof.scoped, expected, false, "local-baseline")
	clearRelationshipFamilyBinaryProofOutputs(t, ctx, proof.scoped)
	createRelationshipFamilyProofIndex(t, ctx, proof.scoped)
	result := executeRelationshipFamilyBinaryProof(t, ctx, proof.scoped, expected, false, "local-candidate")
	persisted := loadRelationshipFamilyBinaryProofEvidence(t, ctx, proof.scoped, repositories)
	if !evidenceSetsEqual(expectedEvidence, persisted) {
		t.Fatalf("persisted local evidence differs from production Odù evidence: expected=%d persisted=%d",
			len(expectedEvidence), len(persisted))
	}
	t.Logf("relationship-family local binary proof completed: baseline=%s candidate=%s", baseline.summary, result.summary)
}

func runRelationshipFamilyRetainedBinaryProof(t *testing.T, dsn string) {
	t.Helper()
	mode := strings.TrimSpace(os.Getenv(relationshipFamilyFullBackfillProofModeEnv))
	timeout := 10 * time.Minute
	if mode == "baseline" {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open retained binary proof database: %v", err)
	}
	db.SetMaxOpenConns(96)
	db.SetMaxIdleConns(10)
	t.Cleanup(func() { _ = db.Close() })

	var database, schema string
	if err := db.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&database, &schema); err != nil {
		t.Fatalf("read retained proof database identity: %v", err)
	}
	if err := validateRelationshipFamilyBinaryProofDatabase(
		database,
		schema,
		mode,
		strings.TrimSpace(os.Getenv(relationshipFamilyFullBackfillProofConfirmEnv)),
	); err != nil {
		t.Fatal(err)
	}

	expected := relationshipFamilyBinaryProofExpected{
		facts:          relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedFactsEnv),
		references:     relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedRefsEnv),
		partitions:     relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedPartsEnv),
		loaded:         relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedLoadEnv),
		evidence:       relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedEvidEnv),
		readiness:      relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedReadyEnv),
		memos:          relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedMemosEnv),
		queueSucceeded: relationshipFamilyBinaryProofExpectedInt(t, relationshipFamilyFullBackfillExpectedQueueEnv),
		maxDuration:    timeout,
		minReadCalls:   8,
		minReadCursors: 1,
		minWriteTx:     8,
		minWriteCalls:  8,
	}
	result := executeRelationshipFamilyBinaryProof(t, ctx, db, expected, true, mode)
	t.Logf("relationship-family retained %s binary proof completed: %s", mode, result.summary)
}

func executeRelationshipFamilyBinaryProof(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expected relationshipFamilyBinaryProofExpected,
	checkQueue bool,
	mode string,
) relationshipFamilyBinaryProofResult {
	t.Helper()
	assertRelationshipFamilyBinaryProofPreconditions(t, ctx, db, expected, checkQueue)
	assertRelationshipFamilyBinaryProofManifestState(t, ctx, db, mode, expected)
	inputs := captureRelationshipFamilyBinaryProofInputs(t, ctx, db)
	baselineMode := mode == "baseline" || mode == "local-baseline"
	if mode == "candidate" || mode == "local-candidate" {
		assertRelationshipFamilyBinaryProofInputsMatch(t, ctx, db, inputs)
	}
	if !baselineMode {
		assertRelationshipFamilyBinaryProofIndex(t, ctx, db)
	}

	instrumented := &relationshipFamilyBinaryProofDB{SQLDB: SQLDB{DB: db}}
	if baselineMode {
		instrumented.queryOverride = relationshipFamilyOldQuery(t)
	}
	store := NewIngestionStore(instrumented)
	if store.maintenanceWorkers != 8 {
		t.Fatalf("deferred backfill workers = %d, want 8", store.maintenanceWorkers)
	}
	recorder, tracer := recordingTracer()
	started := time.Now()
	if err := store.BackfillAllRelationshipEvidence(ctx, tracer, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > expected.maxDuration {
		t.Fatalf("binary backfill duration = %s, want <= %s", elapsed, expected.maxDuration)
	}

	span := findEndedSpan(t, recorder.Ended(), backfillDeferredSpanName)
	if got := spanIntAttr(t, span, "partition_count"); got != expected.partitions {
		t.Fatalf("partition_count = %d, want %d", got, expected.partitions)
	}
	if got := spanIntAttr(t, span, "worker_count"); got != 8 {
		t.Fatalf("worker_count = %d, want 8", got)
	}
	assertRelationshipFamilyBinaryProofConcurrency(t, instrumented, expected)
	assertRelationshipFamilyBinaryProofOutputs(t, ctx, db, expected, checkQueue)
	loadedIDs, duplicates := instrumented.loadedIDs.snapshot()
	if duplicates != 0 || int64(len(loadedIDs)) != expected.loaded {
		t.Fatalf("loaded fact IDs unique=%d duplicates=%d, want %d/0", len(loadedIDs), duplicates, expected.loaded)
	}
	switch mode {
	case "baseline", "local-baseline":
		writeRelationshipFamilyBinaryProofManifest(t, ctx, db, loadedIDs, elapsed, inputs)
	case "candidate", "local-candidate":
		assertRelationshipFamilyBinaryProofManifest(t, ctx, db, loadedIDs, elapsed, mode == "candidate")
	}

	return relationshipFamilyBinaryProofResult{elapsed: elapsed, summary: fmt.Sprintf(
		"duration=%s facts=%d refs=%d partitions=%d loaded=%d evidence=%d readiness=%d memos=%d "+
			"read_call_peak=%d cursor_peak=%d write_tx_peak=%d write_call_peak=%d",
		elapsed,
		expected.facts,
		expected.references,
		expected.partitions,
		instrumented.loadedFacts.Load(),
		expected.evidence,
		expected.readiness,
		expected.memos,
		instrumented.readCalls.peak(),
		instrumented.readCursors.peak(),
		instrumented.writeTx.peak(),
		instrumented.writeCalls.peak(),
	)}
}

func clearRelationshipFamilyBinaryProofOutputs(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
TRUNCATE TABLE relationship_evidence_facts, graph_projection_phase_state,
               deferred_backfill_partition_memo
`); err != nil {
		t.Fatalf("clear baseline binary proof outputs: %v", err)
	}
}

func assertRelationshipFamilyBinaryProofConcurrency(
	t *testing.T,
	db *relationshipFamilyBinaryProofDB,
	expected relationshipFamilyBinaryProofExpected,
) {
	t.Helper()
	if got := db.queryTasks.Load(); got != expected.partitions {
		t.Fatalf("deferred query tasks = %d, want %d", got, expected.partitions)
	}
	if expected.loaded >= 0 && db.loadedFacts.Load() != expected.loaded {
		t.Fatalf("loaded facts = %d, want %d", db.loadedFacts.Load(), expected.loaded)
	}
	for name, overlap := range map[string]struct {
		got  int
		want int
	}{
		"read call peak":  {got: db.readCalls.peak(), want: expected.minReadCalls},
		"cursor peak":     {got: db.readCursors.peak(), want: expected.minReadCursors},
		"write tx peak":   {got: db.writeTx.peak(), want: expected.minWriteTx},
		"write call peak": {got: db.writeCalls.peak(), want: expected.minWriteCalls},
	} {
		if overlap.got < overlap.want {
			t.Fatalf("%s = %d, want >= %d", name, overlap.got, overlap.want)
		}
	}
	if db.readCalls.active() != 0 || db.readCursors.active() != 0 ||
		db.writeTx.active() != 0 || db.writeCalls.active() != 0 {
		t.Fatal("binary proof adapter retained an active read, cursor, transaction, or write call")
	}
}

func relationshipFamilyBinaryProofExpectedInt(t *testing.T, name string) int64 {
	t.Helper()
	value, err := parseRelationshipFamilyBinaryProofPositiveInt(os.Getenv(name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value
}

func countRelationshipFamilyBinaryProofRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	table string,
) int64 {
	t.Helper()
	var count int64
	query := "SELECT count(*) FROM " + quoteSQLIdentifier(table)
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func loadRelationshipFamilyBinaryProofEvidence(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	repositories map[string]repositoryGenerationIdentity,
) []relationships.EvidenceFact {
	t.Helper()
	generations := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		generations[repository.GenerationID] = struct{}{}
	}
	store := NewRelationshipStore(SQLDB{DB: db})
	var result []relationships.EvidenceFact
	for generationID := range generations {
		evidence, err := store.ListEvidenceFacts(ctx, generationID)
		if err != nil {
			t.Fatalf("list evidence for generation %q: %v", generationID, err)
		}
		result = append(result, evidence...)
	}
	return result
}

func relationshipFamilyBinaryProofReadinessCount(t *testing.T, ctx context.Context, db *sql.DB) int64 {
	t.Helper()
	var count int64
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM graph_projection_phase_state
WHERE keyspace = $1 AND phase = $2
`,
		string(reducer.GraphProjectionKeyspaceCrossRepoEvidence),
		string(reducer.GraphProjectionPhaseBackwardEvidenceCommitted),
	).Scan(&count); err != nil {
		t.Fatalf("count backward-evidence readiness: %v", err)
	}
	return count
}

func assertRelationshipFamilyBinaryProofIndex(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var valid, ready bool
	var size int64
	var predicate string
	if err := db.QueryRowContext(ctx,
		relationshipFamilyBinaryProofIndexStateQuery,
		relationshipFamilyProofIndexName,
	).Scan(&valid, &ready, &size, &predicate); err != nil {
		t.Fatalf("read relationship-family index state: %v", err)
	}
	if !valid || !ready || size <= 0 {
		t.Fatalf("relationship-family index valid=%v ready=%v size=%d", valid, ready, size)
	}
	for _, marker := range []string{"gcp_cloud_relationship", "artifact_type", "gitfs_remotes"} {
		if !strings.Contains(predicate, marker) {
			t.Fatalf("relationship-family index predicate missing %q: %s", marker, predicate)
		}
	}
}

func assertRelationshipFamilyBinaryProofPreconditions(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expected relationshipFamilyBinaryProofExpected,
	checkQueue bool,
) {
	t.Helper()
	if got := countRelationshipFamilyBinaryProofRows(t, ctx, db, "fact_records"); got != expected.facts {
		t.Fatalf("fact rows = %d, want %d", got, expected.facts)
	}
	if got := countRelationshipFamilyBinaryProofRows(t, ctx, db, "relationship_reference_candidate_keys"); got != expected.references {
		t.Fatalf("reference rows = %d, want %d", got, expected.references)
	}
	partitions, err := loadActiveScopeGenerationPartitions(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("load retained proof partitions: %v", err)
	}
	if int64(len(partitions)) != expected.partitions {
		t.Fatalf("active partitions = %d, want %d", len(partitions), expected.partitions)
	}
	for table, count := range map[string]int64{
		"relationship_evidence_facts":      countRelationshipFamilyBinaryProofRows(t, ctx, db, "relationship_evidence_facts"),
		"deferred_backfill_partition_memo": countRelationshipFamilyBinaryProofRows(t, ctx, db, "deferred_backfill_partition_memo"),
		"backward-evidence readiness rows": relationshipFamilyBinaryProofReadinessCount(t, ctx, db),
	} {
		if count != 0 {
			t.Fatalf("refusing already-run binary proof: %s = %d, want 0", table, count)
		}
	}
	if checkQueue {
		assertRelationshipFamilyBinaryProofQueue(t, ctx, db, expected.queueSucceeded)
	}
}

func assertRelationshipFamilyBinaryProofOutputs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expected relationshipFamilyBinaryProofExpected,
	checkQueue bool,
) {
	t.Helper()
	var evidence, distinctEvidence int64
	if err := db.QueryRowContext(ctx,
		"SELECT count(*), count(DISTINCT evidence_id) FROM relationship_evidence_facts",
	).Scan(&evidence, &distinctEvidence); err != nil {
		t.Fatalf("count persisted relationship evidence: %v", err)
	}
	if evidence != expected.evidence || distinctEvidence != evidence {
		t.Fatalf("evidence rows=%d distinct=%d, want %d unique", evidence, distinctEvidence, expected.evidence)
	}
	if got := relationshipFamilyBinaryProofReadinessCount(t, ctx, db); got != expected.readiness {
		t.Fatalf("readiness rows = %d, want %d", got, expected.readiness)
	}
	if got := countRelationshipFamilyBinaryProofRows(t, ctx, db, "deferred_backfill_partition_memo"); got != expected.memos {
		t.Fatalf("memo rows = %d, want %d", got, expected.memos)
	}
	if checkQueue {
		assertRelationshipFamilyBinaryProofQueue(t, ctx, db, expected.queueSucceeded)
	}
}

func assertRelationshipFamilyBinaryProofQueue(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expectedSucceeded int64,
) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT status, count(*) FROM fact_work_items GROUP BY status")
	if err != nil {
		t.Fatalf("read proof queue state: %v", err)
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan proof queue state: %v", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate proof queue state: %v", err)
	}
	if counts["succeeded"] != expectedSucceeded {
		t.Fatalf("succeeded queue rows = %d, want %d", counts["succeeded"], expectedSucceeded)
	}
	for status, count := range counts {
		if status != "succeeded" && count != 0 {
			t.Fatalf("queue status %q has %d rows, want 0", status, count)
		}
	}
}
