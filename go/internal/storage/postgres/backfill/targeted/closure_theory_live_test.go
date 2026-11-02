// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Read-side parity proof for partition-scoped deferred maintenance
// (#7584). It runs only against the disposable PostgreSQL named by
// ESHU_TARGETED_MAINTENANCE_PROOF_DSN with
// ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1.
package targeted_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7584 disposable PostgreSQL 18 read-side parity proof that the target-catalog inbound load plus the exact owed partition load equals the full deferred loader for evidence touching the owed repository; enrolled in the live-postgres-readiness runner; local run passed in 0.30s of test time on the pinned postgres:18 image (2 CPUs, 1 GiB) on 2026-10-05; wall time is logged, not gated
//eshu:readiness:env ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE ESHU_TARGETED_MAINTENANCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill/targeted"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestTheoryExactPartitionEvidenceClosure compares a bounded candidate read
// with the real fleet loader for evidence touching one active repository.
// It does not prove writes, phase publication, reopen, or representative cost.
func TestTheoryExactPartitionEvidenceClosure(t *testing.T) {
	dsn := targetedMaintenanceProofDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	database := testfixtures.OpenDeferredPartitionMemoProofDB(t, dsn)
	schemaName := testfixtures.ProvisionDeferredPartitionMemoSchema(t, database)
	t.Logf("private proof schema=%s", schemaName)
	testfixtures.SeedDeferredPartitionFixture(t, ctx, database)
	testfixtures.SeedMemoProofScopesAndFacts(t, ctx, database, []testfixtures.MemoProofFixture{
		{
			ScopeID: "git:theory-gcp-source", GenID: "gen-theory-gcp-source",
			RepoID: "repo-gcp-source", RepoName: "order-gateway",
		},
		{
			ScopeID: "git:theory-gcp-target", GenID: "gen-theory-gcp-target",
			RepoID: "repo-gcp-target", RepoName: "payments-service",
		},
	}, map[string]string{"repo-gcp-target": "shared-id"},
		time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	insertInbound := func(factID, scopeID, generationID, sourceRepoID, path string) {
		t.Helper()
		payload, err := json.Marshal(map[string]string{
			"repo_id":       sourceRepoID,
			"artifact_type": "terraform",
			"relative_path": path,
			"content":       "app_repo = \"payments-service\"",
		})
		if err != nil {
			t.Fatalf("marshal inbound fact %s: %v", factID, err)
		}
		const insertFact = "INSERT INTO fact_records " +
			"(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload) " +
			"VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)"
		at := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
		if _, err := database.ExecContext(ctx, insertFact,
			factID, scopeID, generationID, at, string(payload)); err != nil {
			t.Fatalf("seed inbound fact %s: %v", factID, err)
		}
	}
	insertInbound("fact-alpha-target", "git:scope-a", "gen-a", "shared-id", "alpha-target.tf")
	insertInbound("fact-beta-target", "git:scope-b", "gen-b", "shared-id", "beta-target.tf")
	insertInbound("fact-ref-target", "git:scope-ref", "gen-ref", "repo-ref-target", "ref-target.tf")

	if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes AS scope
SET active_generation_id = generation.generation_id
FROM scope_generations AS generation
WHERE generation.scope_id = scope.scope_id`); err != nil {
		t.Fatalf("seed active pointers: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		"UPDATE scope_generations SET status = 'active'"); err != nil {
		t.Fatalf("seed active statuses: %v", err)
	}

	adapter := postgres.SQLDB{DB: database}
	engine := backfill.NewStore(adapter)
	engine.Workers = 1
	catalog, _, err := backfill.LoadRepositoryCatalog(ctx, adapter)
	if err != nil {
		t.Fatalf("load actual repository catalog: %v", err)
	}
	fullFacts, _, _, err := engine.LoadDeferredAnchorScopedRelationshipFacts(
		ctx, adapter, catalog, nil)
	if err != nil {
		t.Fatalf("load actual full maintenance facts: %v", err)
	}

	targetRepoID := "repo-gcp-target"
	targetCatalog := backfill.RepositoryScopedCatalog(catalog,
		map[string]struct{}{targetRepoID: {}})
	inboundFacts, err := backfill.LoadAnchorScopedRelationshipFacts(
		ctx, adapter, targetCatalog, catalog)
	if err != nil {
		t.Fatalf("load inbound target-anchored facts: %v", err)
	}
	params, ok := backfill.BuildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("fixture has no full-catalog anchors")
	}
	exactPartition := backfill.ScopeGenerationPartition{
		ScopeID: "git:theory-gcp-target", GenerationID: "gen-theory-gcp-target",
	}
	ownFacts, _, err := engine.LoadDeferredScopedFactsAcrossPartitions(
		ctx, adapter, params, []backfill.ScopeGenerationPartition{exactPartition}, nil)
	if err != nil {
		t.Fatalf("load exact target Partition: %v", err)
	}
	if len(ownFacts) == 0 {
		t.Fatal("exact target partition loaded no outgoing source facts")
	}
	candidateFacts := backfill.MergeRelationshipFacts(ownFacts, inboundFacts)
	candidateFacts, err = engine.AppendArgoCDGeneratorConfigFacts(
		ctx, adapter, catalog, candidateFacts)
	if err != nil {
		t.Fatalf("load external ArgoCD config closure: %v", err)
	}

	requiredFactIDs := []string{
		"content-repo-gcp-target",
		"fact-gcp-edge",
		"fact-alpha-target",
		"fact-beta-target",
		"fact-ref-target",
	}
	for _, input := range []struct {
		name      string
		envelopes []facts.Envelope
	}{
		{"full", fullFacts},
		{"candidate", candidateFacts},
	} {
		for _, factID := range requiredFactIDs {
			found := false
			for _, envelope := range input.envelopes {
				if envelope.FactID == factID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s fact load missed required fact ID %s", input.name, factID)
			}
		}
	}

	relevant := func(envelopes []facts.Envelope) map[string]int {
		t.Helper()
		result := make(map[string]int)
		evidence := relationships.DedupeEvidenceFacts(
			relationships.DiscoverEvidence(envelopes, catalog))
		for _, item := range evidence {
			if item.SourceRepoID != targetRepoID &&
				item.TargetRepoID != targetRepoID {
				continue
			}
			encoded, err := json.Marshal(item)
			if err != nil {
				t.Fatalf("marshal evidence: %v", err)
			}
			result[string(encoded)]++
		}
		return result
	}
	fullEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(fullFacts, catalog))
	if !theoryEvidenceHasEdge(fullEvidence, "repo-gcp-source", targetRepoID) {
		t.Fatal("full loader missed cloud-scope GCP evidence; fixture is invalid")
	}
	expected := []struct {
		source string
		target string
		path   string
	}{
		{"repo-gcp-target", "shared-id", "main.tf"},
		{"repo-ref-target", targetRepoID, "ref-target.tf"},
		{"shared-id", targetRepoID, "alpha-target.tf"},
		{"shared-id", targetRepoID, "beta-target.tf"},
	}
	for _, want := range expected {
		found := false
		for _, evidence := range fullEvidence {
			if evidence.SourceRepoID == want.source &&
				evidence.TargetRepoID == want.target &&
				evidence.Details["path"] == want.path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("full loader missed required evidence source=%s target=%s path=%s",
				want.source, want.target, want.path)
		}
	}

	full := relevant(fullFacts)
	candidate := relevant(candidateFacts)
	if len(full) == 0 {
		t.Fatal("full maintenance produced no target-relevant evidence")
	}
	if !reflect.DeepEqual(candidate, full) {
		t.Fatalf("target evidence differs: candidate=%v full=%v", candidate, full)
	}
}

func theoryEvidenceHasEdge(evidence []relationships.EvidenceFact, source, target string) bool {
	for _, fact := range evidence {
		if fact.SourceRepoID == source && fact.TargetRepoID == target {
			return true
		}
	}
	return false
}

// seedParityCorpus writes the anchor parity corpus into fact_records and points
// every scope at its generation.
func seedParityCorpus(t *testing.T, ctx context.Context, database *sql.DB) []targeted.ParityRow {
	t.Helper()
	at := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	rows := targeted.ParityRows(t)
	seeded := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seeded[row.ScopeID]; !ok {
			seeded[row.ScopeID] = struct{}{}
			testfixtures.SeedMemoProofScopesAndFacts(t, ctx, database, []testfixtures.MemoProofFixture{{
				ScopeID: row.ScopeID, GenID: row.GenerationID, RepoID: row.ScopeID, RepoName: row.ScopeID,
			}}, nil, at)
		}
		const insertFact = "INSERT INTO fact_records " +
			"(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, source_system, source_fact_key, observed_at, ingested_at, payload) " +
			"VALUES ($1, $2, $3, $4, $8, $5, 'git', $1, $6, $6, $7::jsonb)"
		if _, err := database.ExecContext(ctx, insertFact,
			row.FactID, row.ScopeID, row.GenerationID, row.Kind, row.SchemaVersion, at, string(row.Payload), row.StableFactKey); err != nil {
			t.Fatalf("seed parity fact %s: %v", row.FactID, err)
		}
	}
	if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes AS scope
SET active_generation_id = generation.generation_id
FROM scope_generations AS generation
WHERE generation.scope_id = scope.scope_id`); err != nil {
		t.Fatalf("seed active pointers: %v", err)
	}
	return rows
}

func countLoaded(loaded []facts.Envelope, prefix string) int {
	count := 0
	for _, envelope := range loaded {
		if strings.HasPrefix(envelope.FactID, prefix) {
			count++
		}
	}
	return count
}

func loadedIDs(loaded []facts.Envelope) []string {
	ids := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		ids = append(ids, envelope.FactID)
	}
	sort.Strings(ids)
	return ids
}

// TestAnchorScopedLoadKeepsOwedEvidenceOnPostgres runs the anchor parity corpus
// through the real statement: jsonb text rendering, the lower() call, and the
// backslash-escaped underscore in an r_ LIKE term are all things the in-memory
// parity tests only model. Evidence must equal the fleet answer. No noise or
// negative row may load, and the ArgoCD file rows must, including a payload whose
// spacing only jsonb normalization removes.
func TestAnchorScopedLoadKeepsOwedEvidenceOnPostgres(t *testing.T) {
	dsn := targetedMaintenanceProofDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	database := testfixtures.OpenDeferredPartitionMemoProofDB(t, dsn)
	testfixtures.ProvisionDeferredPartitionMemoSchema(t, database)
	rows := seedParityCorpus(t, ctx, database)

	catalog := targeted.ParityCatalog()
	owed := targeted.ParityOwed()
	loaded, err := backfill.LoadAnchorScopedRelationshipFacts(
		ctx, postgres.SQLDB{DB: database}, backfill.RepositoryScopedCatalog(catalog, owed), catalog)
	if err != nil {
		t.Fatalf("LoadAnchorScopedRelationshipFacts: %v", err)
	}

	fleet := targeted.ParityFleetEvidence(t, catalog, owed)
	got := targeted.ParityTouchingOwed(
		relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(loaded, catalog)), owed)
	if !reflect.DeepEqual(got, fleet) {
		t.Errorf("Postgres evidence differs from fleet evidence\ngot (%d):   %v\nfleet (%d): %v", len(got), got, len(fleet), fleet)
	}

	noiseContent, noiseFile, negative := countLoaded(loaded, "noise-content-"), countLoaded(loaded, "noise-file-"), countLoaded(loaded, "negative-")
	if noiseContent != 0 || noiseFile != 0 || negative != 0 {
		t.Errorf("noise_content=%d noise_file=%d negative=%d, want 0; loaded %v", noiseContent, noiseFile, negative, loadedIDs(loaded))
	}
	for _, factID := range []string{"marker-application-file", "marker-applicationset-file", "marker-spaced-full", "config-file-only"} {
		if countLoaded(loaded, factID) != 1 {
			t.Errorf("%s did not load; loaded %v", factID, loadedIDs(loaded))
		}
	}
	t.Logf("corpus=%d loaded=%d fleet_edges=%d got_edges=%d noise_content=%d noise_file=%d negative=%d",
		len(rows), len(loaded), len(fleet), len(got), noiseContent, noiseFile, negative)
}

// TestDeferredLoadKeepsEvidenceOnPostgres runs the same corpus through the
// deferred statement, whose family predicate decides which rows reach the anchor
// test at all. The reference is the same statement with the old key-name terms
// added back, so the comparison isolates the marker change and any route those
// terms were hiding shows up as lost evidence. It compares the whole catalog,
// since this pass owes every repository.
func TestDeferredLoadKeepsEvidenceOnPostgres(t *testing.T) {
	dsn := targetedMaintenanceProofDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	database := testfixtures.OpenDeferredPartitionMemoProofDB(t, dsn)
	testfixtures.ProvisionDeferredPartitionMemoSchema(t, database)
	rows := seedParityCorpus(t, ctx, database)

	catalog := targeted.ParityCatalog()
	every := make(map[string]struct{}, len(catalog))
	for _, entry := range catalog {
		every[entry.RepoID] = struct{}{}
	}
	adapter := postgres.SQLDB{DB: database}
	engine := backfill.NewStore(adapter)
	loaded, _, _, err := engine.LoadDeferredAnchorScopedRelationshipFacts(ctx, adapter, catalog, nil)
	if err != nil {
		t.Fatalf("LoadDeferredAnchorScopedRelationshipFacts: %v", err)
	}

	params, ok := backfill.BuildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("corpus catalog built no deferred params")
	}
	params.NonRepoIDLike = append(params.NonRepoIDLike, `%argocd\_applications%`, `%argocd\_applicationsets%`)
	partitions, err := backfill.LoadActiveScopeGenerationPartitions(ctx, adapter)
	if err != nil {
		t.Fatalf("LoadActiveScopeGenerationPartitions: %v", err)
	}
	wide, _, err := engine.LoadDeferredScopedFactsAcrossPartitions(ctx, adapter, params, partitions, nil)
	if err != nil {
		t.Fatalf("load with the key-name terms: %v", err)
	}
	wide, err = engine.AppendArgoCDGeneratorConfigFacts(ctx, adapter, catalog, wide)
	if err != nil {
		t.Fatalf("append config facts: %v", err)
	}

	got := targeted.ParityTouchingOwed(relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(loaded, catalog)), every)
	want := targeted.ParityTouchingOwed(relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(wide, catalog)), every)
	if lost := targeted.ParityMissing(want, got); len(lost) != 0 {
		t.Errorf("the load lost evidence the key-name load kept: %v", lost)
	}
	if noise := countLoaded(loaded, "noise-file-") + countLoaded(loaded, "negative-"); noise != 0 {
		t.Errorf("%d noise or negative rows loaded: %v", noise, loadedIDs(loaded))
	}
	if countLoaded(loaded, "marker-unresolved") != 1 {
		t.Errorf("marker-unresolved did not load, so the deferred terms lost the ArgoCD markers; loaded %v", loadedIDs(loaded))
	}
	if countLoaded(wide, "negative-admitted-path") != 1 {
		t.Errorf("the key-name load should select negative-admitted-path, or this test proves no narrowing; loaded %v", loadedIDs(wide))
	}

	// What is still missing against the fleet is the family predicate's limit, not
	// the anchors': it admits a file row by path or Flux bucket, and these rows have
	// neither. The real parser's content fact for the same manifest carries it.
	fleet := targeted.ParityFleetEvidence(t, catalog, every)
	notAdmitted := targeted.ParityMissing(fleet, got)
	for _, row := range notAdmitted {
		if !strings.Contains(row, "|apps/") || !strings.HasPrefix(row, "ARGOCD_") {
			t.Errorf("evidence outside the known family predicate limit is missing: %s", row)
		}
	}
	t.Logf("corpus=%d loaded=%d wide_loaded=%d edges=%d wide_edges=%d fleet_edges=%d not_admitted=%d",
		len(rows), len(loaded), len(wide), len(got), len(want), len(fleet), len(notAdmitted))
}
