// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
)

type relationshipFamilyWriteTaxProof struct {
	admin           *sql.DB
	control         *sql.DB
	candidate       *sql.DB
	controlSchema   string
	candidateSchema string
}

type relationshipFamilyWriteTaxResult struct {
	duration      time.Duration
	peakWriters   int32
	acceptedRows  int
	rows          int
	sourceRows    int
	familyRows    int
	duplicateRows int
	failures      int
}

type relationshipFamilyWriteTaxWorkerResult struct {
	accepted int
	err      error
}

func openRelationshipFamilyWriteTaxProof(t *testing.T) relationshipFamilyWriteTaxProof {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(relationshipFamilyIndexProofDSNEnv))
	if dsn == "" {
		t.Skip("set ESHU_RELATIONSHIP_FAMILY_INDEX_PROOF_DSN to run the local relationship-family index write-tax proof")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open write-tax postgres: %v", err)
	}
	var database string
	if err := admin.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		_ = admin.Close()
		t.Fatalf("read write-tax database name: %v", err)
	}
	if database != relationshipFamilyProofDatabase {
		_ = admin.Close()
		t.Fatalf("refusing write-tax proof against database %q; want dedicated %q", database, relationshipFamilyProofDatabase)
	}

	suffix := time.Now().UnixNano()
	proof := relationshipFamilyWriteTaxProof{
		admin:           admin,
		controlSchema:   fmt.Sprintf("relationship_family_write_control_%d", suffix),
		candidateSchema: fmt.Sprintf("relationship_family_write_candidate_%d", suffix),
	}
	t.Cleanup(func() {
		if proof.control != nil {
			_ = proof.control.Close()
		}
		if proof.candidate != nil {
			_ = proof.candidate.Close()
		}
		_, _ = proof.admin.ExecContext(context.Background(), "DROP SCHEMA "+quoteSQLIdentifier(proof.controlSchema)+" CASCADE")
		_, _ = proof.admin.ExecContext(context.Background(), "DROP SCHEMA "+quoteSQLIdentifier(proof.candidateSchema)+" CASCADE")
		_ = proof.admin.Close()
	})
	proof.control = openRelationshipFamilyWriteTaxSchema(t, dsn, proof.controlSchema)
	proof.candidate = openRelationshipFamilyWriteTaxSchema(t, dsn, proof.candidateSchema)
	seedRelationshipFamilyWriteTaxCoordinates(t, ctx, proof.control)
	seedRelationshipFamilyWriteTaxCoordinates(t, ctx, proof.candidate)
	createRelationshipFamilyProofIndex(t, ctx, proof.candidate)
	assertRelationshipFamilyWriteTaxPersistence(t, ctx, proof.control, false)
	assertRelationshipFamilyWriteTaxPersistence(t, ctx, proof.candidate, true)

	return proof
}

func openRelationshipFamilyWriteTaxSchema(t *testing.T, dsn, schema string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open write-tax schema admin: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		_ = admin.Close()
		t.Fatalf("create write-tax schema %q: %v", schema, err)
	}
	_ = admin.Close()

	db, err := sql.Open("pgx", relationshipFamilyDSNWithSearchPath(t, dsn, schema))
	if err != nil {
		t.Fatalf("open write-tax schema %q: %v", schema, err)
	}
	db.SetMaxOpenConns(relationshipFamilyWriteTaxWorkers + 2)
	db.SetMaxIdleConns(relationshipFamilyWriteTaxWorkers + 2)
	if _, err := db.ExecContext(ctx, deferredPartitionProofSchemaSQL); err != nil {
		_ = db.Close()
		t.Fatalf("create write-tax tables in %q: %v", schema, err)
	}
	return db
}

func seedRelationshipFamilyWriteTaxCoordinates(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	observedAt := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	for worker := 0; worker < relationshipFamilyWriteTaxWorkers; worker++ {
		scopeID := fmt.Sprintf("scope:relationship-family-write-tax:%02d", worker)
		generationID := fmt.Sprintf("generation:relationship-family-write-tax:%02d", worker)
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, $2)",
			scopeID, generationID); err != nil {
			t.Fatalf("seed write-tax scope %d: %v", worker, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			generationID, scopeID, observedAt); err != nil {
			t.Fatalf("seed write-tax generation %d: %v", worker, err)
		}
	}
}

func assertRelationshipFamilyWriteTaxPersistence(t *testing.T, ctx context.Context, db *sql.DB, wantIndex bool) {
	t.Helper()
	var tablePersistence string
	if err := db.QueryRowContext(ctx,
		"SELECT relpersistence::text FROM pg_class WHERE oid = 'fact_records'::regclass",
	).Scan(&tablePersistence); err != nil {
		t.Fatalf("read fact_records persistence: %v", err)
	}
	if tablePersistence != "p" {
		t.Fatalf("fact_records persistence = %q, want ordinary WAL-backed p", tablePersistence)
	}
	var indexCount int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_class WHERE oid = to_regclass($1) AND relpersistence = 'p'",
		relationshipFamilyProofIndexName,
	).Scan(&indexCount); err != nil {
		t.Fatalf("read candidate index persistence: %v", err)
	}
	wantCount := 0
	if wantIndex {
		wantCount = 1
	}
	if indexCount != wantCount {
		t.Fatalf("ordinary candidate indexes = %d, want %d", indexCount, wantCount)
	}
}

func relationshipFamilyWriteTaxFixture(t *testing.T) []facts.Envelope {
	t.Helper()
	odu := ifa.RepoDependencyBackfillProofOdu()
	var familyTemplates, genericTemplates []facts.Envelope
	for _, fact := range odu.Facts {
		switch {
		case strings.HasPrefix(fact.StableFactKey, "backfill-generic-distractor:"):
			genericTemplates = append(genericTemplates, fact)
		case fact.FactKind == "content" || fact.FactKind == "file" || fact.FactKind == facts.GCPCloudRelationshipFactKind:
			familyTemplates = append(familyTemplates, fact)
		}
	}
	if len(familyTemplates) == 0 || len(genericTemplates) == 0 {
		t.Fatalf("relationship-family Odù templates family=%d generic=%d", len(familyTemplates), len(genericTemplates))
	}
	fixture := make([]facts.Envelope, 0, relationshipFamilyWriteTaxFacts)
	for index := 0; index < relationshipFamilyWriteTaxFamilyFacts; index++ {
		fixture = append(fixture, cloneRelationshipFamilyWriteTaxFact(familyTemplates[index%len(familyTemplates)], "family", index, ""))
	}
	for index := 0; index < relationshipFamilyWriteTaxSourceFacts-relationshipFamilyWriteTaxFamilyFacts; index++ {
		fixture = append(fixture, cloneRelationshipFamilyWriteTaxFact(genericTemplates[index%len(genericTemplates)], "generic", index, ""))
	}
	for index := 0; len(fixture) < relationshipFamilyWriteTaxFacts; index++ {
		fixture = append(fixture, cloneRelationshipFamilyWriteTaxFact(genericTemplates[index%len(genericTemplates)], "non-source", index, "repository"))
	}
	return fixture
}

func cloneRelationshipFamilyWriteTaxFact(template facts.Envelope, class string, index int, factKind string) facts.Envelope {
	cloned := template.Clone()
	worker := index % relationshipFamilyWriteTaxWorkers
	cloned.ScopeID = fmt.Sprintf("scope:relationship-family-write-tax:%02d", worker)
	cloned.GenerationID = fmt.Sprintf("generation:relationship-family-write-tax:%02d", worker)
	cloned.FactID = fmt.Sprintf("fact:relationship-family-write-tax:%s:%06d", class, index)
	cloned.StableFactKey = fmt.Sprintf("relationship-family-write-tax:%s:%06d", class, index)
	cloned.SourceRef.ScopeID = cloned.ScopeID
	cloned.SourceRef.GenerationID = cloned.GenerationID
	cloned.SourceRef.FactKey = cloned.StableFactKey
	cloned.SourceRef.SourceRecordID = cloned.FactID
	cloned.SourceRef.SourceURI = "synthetic://relationship-family-write-tax/" + class
	if factKind != "" {
		cloned.FactKind = factKind
	}
	return cloned
}

func assertRelationshipFamilyWriteTaxFixture(t *testing.T, fixture []facts.Envelope) {
	t.Helper()
	if len(fixture) != relationshipFamilyWriteTaxFacts {
		t.Fatalf("write-tax fixture facts = %d, want %d", len(fixture), relationshipFamilyWriteTaxFacts)
	}
	sourceRows := 0
	for _, fact := range fixture {
		if fact.FactKind == "content" || fact.FactKind == "file" || fact.FactKind == facts.GCPCloudRelationshipFactKind {
			sourceRows++
		}
	}
	if sourceRows != relationshipFamilyWriteTaxSourceFacts {
		t.Fatalf("write-tax source facts = %d, want %d", sourceRows, relationshipFamilyWriteTaxSourceFacts)
	}
}

func runRelationshipFamilyWriteTaxRound(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	fixture []facts.Envelope,
) relationshipFamilyWriteTaxResult {
	t.Helper()
	if _, err := db.ExecContext(ctx, "TRUNCATE fact_records"); err != nil {
		t.Fatalf("truncate write-tax facts: %v", err)
	}
	shards := make([][]facts.Envelope, relationshipFamilyWriteTaxWorkers)
	for index, fact := range fixture {
		worker := index % relationshipFamilyWriteTaxWorkers
		shards[worker] = append(shards[worker], fact)
	}

	transactionReady := make(chan struct{}, relationshipFamilyWriteTaxWorkers)
	writerGate := make(chan struct{})
	writerEntered := make(chan struct{}, relationshipFamilyWriteTaxWorkers)
	queryGate := make(chan struct{})
	results := make(chan relationshipFamilyWriteTaxWorkerResult, relationshipFamilyWriteTaxWorkers)
	var activeWriters, peakWriters atomic.Int32
	for worker := 0; worker < relationshipFamilyWriteTaxWorkers; worker++ {
		go runRelationshipFamilyWriteTaxWorker(
			ctx, db, shards[worker], transactionReady, writerGate, writerEntered, queryGate,
			&activeWriters, &peakWriters, results,
		)
	}
	for worker := 0; worker < relationshipFamilyWriteTaxWorkers; worker++ {
		<-transactionReady
	}
	started := time.Now()
	close(writerGate)
	for worker := 0; worker < relationshipFamilyWriteTaxWorkers; worker++ {
		<-writerEntered
	}
	close(queryGate)

	result := relationshipFamilyWriteTaxResult{peakWriters: peakWriters.Load()}
	for worker := 0; worker < relationshipFamilyWriteTaxWorkers; worker++ {
		workerResult := <-results
		if workerResult.err != nil {
			result.failures++
			t.Logf("relationship-family write-tax worker failure: %v", workerResult.err)
		}
		result.acceptedRows += workerResult.accepted
	}
	result.duration = time.Since(started)
	row := db.QueryRowContext(ctx, `
SELECT count(*),
       count(*) FILTER (WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')),
       count(*) FILTER (WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship') AND `+deferredRelationshipFamilyCandidatePredicateSQL+`),
       count(*) - count(DISTINCT fact_id)
FROM fact_records AS fact`)
	if err := row.Scan(&result.rows, &result.sourceRows, &result.familyRows, &result.duplicateRows); err != nil {
		t.Fatalf("read write-tax result cardinality: %v", err)
	}
	return result
}

func runRelationshipFamilyWriteTaxWorker(
	ctx context.Context,
	db *sql.DB,
	factsToWrite []facts.Envelope,
	transactionReady chan<- struct{},
	writerGate <-chan struct{},
	writerEntered chan<- struct{},
	queryGate <-chan struct{},
	activeWriters, peakWriters *atomic.Int32,
	results chan<- relationshipFamilyWriteTaxWorkerResult,
) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		transactionReady <- struct{}{}
		writerEntered <- struct{}{}
		results <- relationshipFamilyWriteTaxWorkerResult{err: fmt.Errorf("begin write-tax transaction: %w", err)}
		return
	}
	transactionReady <- struct{}{}
	<-writerGate
	active := activeWriters.Add(1)
	for peak := peakWriters.Load(); active > peak && !peakWriters.CompareAndSwap(peak, active); peak = peakWriters.Load() {
	}
	writerEntered <- struct{}{}
	<-queryGate
	defer activeWriters.Add(-1)

	accepted := 0
	for offset := 0; offset < len(factsToWrite); offset += factBatchSize {
		end := min(offset+factBatchSize, len(factsToWrite))
		batchAccepted, batchErr := upsertFactBatchReturningAccepted(ctx, SQLTx{Tx: tx}, factsToWrite[offset:end])
		if batchErr != nil {
			_ = tx.Rollback()
			results <- relationshipFamilyWriteTaxWorkerResult{accepted: accepted, err: batchErr}
			return
		}
		accepted += len(batchAccepted)
	}
	if err := tx.Commit(); err != nil {
		results <- relationshipFamilyWriteTaxWorkerResult{accepted: accepted, err: fmt.Errorf("commit write-tax transaction: %w", err)}
		return
	}
	results <- relationshipFamilyWriteTaxWorkerResult{accepted: accepted}
}

func assertRelationshipFamilyWriteTaxResult(t *testing.T, label string, result relationshipFamilyWriteTaxResult) {
	t.Helper()
	if result.failures != 0 || result.acceptedRows != relationshipFamilyWriteTaxFacts || result.rows != relationshipFamilyWriteTaxFacts ||
		result.sourceRows != relationshipFamilyWriteTaxSourceFacts || result.familyRows != relationshipFamilyWriteTaxFamilyFacts ||
		result.duplicateRows != 0 || result.peakWriters != relationshipFamilyWriteTaxWorkers {
		t.Errorf(
			"%s write-tax result = %+v, want failures=0 accepted=rows=%d source=%d family=%d duplicates=0 peak=%d",
			label, result, relationshipFamilyWriteTaxFacts, relationshipFamilyWriteTaxSourceFacts,
			relationshipFamilyWriteTaxFamilyFacts, relationshipFamilyWriteTaxWorkers,
		)
	}
}

func medianDuration(input []time.Duration) time.Duration {
	values := append([]time.Duration(nil), input...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[len(values)/2]
}
