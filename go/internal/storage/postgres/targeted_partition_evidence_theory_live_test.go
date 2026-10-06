// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Read-side parity proof for partition-scoped deferred maintenance
// (#7584). It runs only against the disposable PostgreSQL named by
// ESHU_TARGETED_MAINTENANCE_PROOF_DSN with
// ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1.
package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestTheoryExactPartitionEvidenceClosure compares a bounded candidate read
// with the real fleet loader for evidence touching one active repository.
// It does not prove writes, phase publication, reopen, or representative cost.
func TestTheoryExactPartitionEvidenceClosure(t *testing.T) {
	dsn := targetedMaintenanceProofDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	database := openDeferredPartitionMemoProofDB(t, dsn)
	schemaName := provisionDeferredPartitionMemoSchema(t, database)
	t.Logf("private proof schema=%s", schemaName)
	seedDeferredPartitionFixture(t, ctx, database)
	seedMemoProofScopesAndFacts(t, ctx, database, []memoProofFixture{
		{
			scopeID: "git:theory-gcp-source", genID: "gen-theory-gcp-source",
			repoID: "repo-gcp-source", repoName: "order-gateway",
		},
		{
			scopeID: "git:theory-gcp-target", genID: "gen-theory-gcp-target",
			repoID: "repo-gcp-target", repoName: "payments-service",
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

	adapter := SQLDB{DB: database}
	store := NewIngestionStore(adapter)
	store.maintenanceWorkers = 1
	catalog, _, err := loadRepositoryCatalog(ctx, adapter)
	if err != nil {
		t.Fatalf("load actual repository catalog: %v", err)
	}
	fullFacts, _, _, err := store.loadDeferredAnchorScopedRelationshipFacts(
		ctx, adapter, catalog, nil)
	if err != nil {
		t.Fatalf("load actual full maintenance facts: %v", err)
	}

	targetRepoID := "repo-gcp-target"
	targetCatalog := repositoryScopedCatalog(catalog,
		map[string]struct{}{targetRepoID: {}})
	inboundFacts, err := loadAnchorScopedRelationshipFacts(
		ctx, adapter, targetCatalog, catalog)
	if err != nil {
		t.Fatalf("load inbound target-anchored facts: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("fixture has no full-catalog anchors")
	}
	exactPartition := scopeGenerationPartition{
		ScopeID: "git:theory-gcp-target", GenerationID: "gen-theory-gcp-target",
	}
	ownFacts, _, err := store.loadDeferredScopedFactsAcrossPartitions(
		ctx, adapter, params, []scopeGenerationPartition{exactPartition}, nil)
	if err != nil {
		t.Fatalf("load exact target partition: %v", err)
	}
	if len(ownFacts) == 0 {
		t.Fatal("exact target partition loaded no outgoing source facts")
	}
	candidateFacts := mergeRelationshipFacts(ownFacts, inboundFacts)
	candidateFacts, err = store.appendArgoCDGeneratorConfigFacts(
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
	if !evidenceHasEdge(fullEvidence, "repo-gcp-source", targetRepoID) {
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
