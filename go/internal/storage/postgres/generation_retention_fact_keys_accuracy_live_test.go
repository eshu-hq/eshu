// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// factKeyEnvelopes builds n facts of each key-owning family for one
// generation: content facts (relationship keys), git dependency Variables
// (manifest consumption keys) and package_registry.package facts (registry
// identity keys). Fact ids embed the generation, as production ids do.
func factKeyEnvelopes(scopeID, generationID string, n int) []facts.Envelope {
	observed := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	envelopes := make([]facts.Envelope, 0, 3*n)
	for i := 0; i < n; i++ {
		base := facts.Envelope{ScopeID: scopeID, GenerationID: generationID, ObservedAt: observed, SourceRef: facts.Ref{SourceSystem: "git"}}
		content := base
		content.FactID, content.FactKind = fmt.Sprintf("%s-content-%03d", generationID, i), "content"
		content.Payload = map[string]any{"repo_id": "repo-keys", "relative_path": fmt.Sprintf("f%d.yaml", i), "content": "image: repo-target"}
		manifest := base
		manifest.FactID, manifest.FactKind = fmt.Sprintf("%s-manifest-%03d", generationID, i), "content_entity"
		manifest.Payload = map[string]any{
			"repo_id": "repo-keys", "entity_type": "Variable", "entity_name": fmt.Sprintf("pkg-%03d", i),
			"entity_metadata": map[string]any{"config_kind": "dependency", "package_manager": "npm"},
		}
		registry := base
		registry.FactID, registry.FactKind = fmt.Sprintf("%s-registry-%03d", generationID, i), "package_registry.package"
		registry.SourceRef = facts.Ref{SourceSystem: "registry"}
		registry.Payload = map[string]any{
			"package_id": fmt.Sprintf("npm://registry.npmjs.org/pkg-%03d", i), "ecosystem": "npm",
			"raw_name": fmt.Sprintf("pkg-%03d", i), "normalized_name": fmt.Sprintf("pkg-%03d", i),
		}
		envelopes = append(envelopes, content, manifest, registry)
	}
	return envelopes
}

// writeFactsWithKeys inserts the envelopes' fact rows and then runs the three
// production key writers on the same executor, the ingestion order
// (ingestion.go: upsertStreamingFacts calls the writers per batch inside the
// commit transaction).
func writeFactsWithKeys(t *testing.T, ctx context.Context, exec db.ExecQueryer, envelopes []facts.Envelope) {
	t.Helper()
	for _, envelope := range envelopes {
		payload, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if _, err := exec.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, $5, $1, $6, $6, $7::jsonb)`,
			envelope.FactID, envelope.ScopeID, envelope.GenerationID, envelope.FactKind,
			envelope.SourceRef.SourceSystem, envelope.ObservedAt, string(payload)); err != nil {
			t.Fatalf("insert fact %s: %v", envelope.FactID, err)
		}
	}
	writeKeysOnly(t, ctx, exec, envelopes)
}

func writeKeysOnly(t *testing.T, ctx context.Context, exec db.ExecQueryer, envelopes []facts.Envelope) {
	t.Helper()
	if err := refreshRelationshipReferenceCandidateKeys(ctx, exec, envelopes); err != nil {
		t.Fatalf("refresh relationship keys: %v", err)
	}
	if err := refreshPackageManifestConsumptionKeys(ctx, exec, envelopes); err != nil {
		t.Fatalf("refresh manifest keys: %v", err)
	}
	if err := refreshPackageRegistryIdentityKeys(ctx, exec, envelopes); err != nil {
		t.Fatalf("refresh registry keys: %v", err)
	}
}

// TestKeyWritersReplaceOrphanFactKeysLive is #7420 accuracy condition (c):
// with the per-fact foreign keys dropped, an orphan key row left for fact X
// under an old generation cannot shadow a fresh write. Every key writer
// deletes by fact_id before it inserts, so after X is written into the new
// generation each table holds X's rows for the new generation only.
func TestKeyWritersReplaceOrphanFactKeysLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	requireFactKeyForeignKeysDropped(t, ctx, database)
	exec := SQLDB{DB: database}
	stale := factKeyEnvelopes("keys-shadow", "keys-shadow-old", 1)
	// The orphans: key rows for the old generation with no fact behind them.
	writeKeysOnly(t, ctx, exec, stale)
	if _, err := database.ExecContext(ctx, `
INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
VALUES ($1, 'keys-shadow', 'keys-shadow-old', 'repo-keys', 'npm', 'pkg-orphan-extra')`, stale[1].FactID); err != nil {
		t.Fatalf("seed extra orphan manifest key: %v", err)
	}
	fresh := make([]facts.Envelope, 0, len(stale))
	for _, envelope := range stale {
		envelope.GenerationID = "keys-shadow-new"
		fresh = append(fresh, envelope)
	}
	writeKeysOnly(t, ctx, exec, fresh)

	for _, tc := range []struct {
		table, factID string
		want          int
	}{
		{"relationship_reference_candidate_keys", stale[0].FactID, 1},
		{"package_manifest_consumption_keys", stale[1].FactID, 1},
		{"package_registry_identity_keys", stale[2].FactID, 1},
	} {
		rows, err := database.QueryContext(ctx, `SELECT generation_id FROM `+tc.table+` WHERE fact_id = $1`, tc.factID)
		if err != nil {
			t.Fatalf("read %s: %v", tc.table, err)
		}
		var generations []string
		for rows.Next() {
			var generation string
			if err := rows.Scan(&generation); err != nil {
				t.Fatalf("scan %s: %v", tc.table, err)
			}
			generations = append(generations, generation)
		}
		_ = rows.Close()
		if len(generations) != tc.want || slices.ContainsFunc(generations, func(g string) bool { return g != "keys-shadow-new" }) {
			t.Errorf("%s rows for %s = %v, want %d row(s) of keys-shadow-new only", tc.table, tc.factID, generations, tc.want)
		}
	}
}

// TestOrphanRelationshipKeysAreInvisibleToDeferredLoadLive is #7420
// accuracy condition (b) for the relationship key reader: the deferred
// relationship fact load (backfill.ListDeferredScopedRelationshipFactRecordsQuery)
// returns facts, joining keys only through a live fact's id, so a key row
// with no fact behind it, even one naming every catalog repository under the
// loaded generation, changes nothing. The package key readers have the same
// proof in go/internal/query/supply/chain/impact.
func TestOrphanRelationshipKeysAreInvisibleToDeferredLoadLive(t *testing.T) {
	dsn := dsnForDeferredHoistProof(t)
	ctx := context.Background()
	db := openDeferredHoistProofDB(t, dsn)
	provisionDeferredHoistSchema(t, db)
	// The fixture seeds its facts' relationship keys itself.
	seedDeferredHoistFixture(t, ctx, db)
	params, ok := backfill.BuildDeferredScopedFactQueryParams(deferredHoistCatalog())
	if !ok {
		t.Fatal("expected usable catalog-derived query params")
	}
	const scope, generation = "git-repository-scope:github.com/org/app", "gen-hoist"
	before := factIDSet(runHoistedDeferredScopedQuery(t, ctx, db, params, scope, generation))
	if len(before) == 0 {
		t.Fatal("baseline deferred load returned no facts")
	}
	var everyRepo []string
	for _, entry := range deferredHoistCatalog() {
		everyRepo = append(everyRepo, entry.RepoID)
	}
	referenceKey := "|" + strings.Join(everyRepo, "|") + "|"
	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(ctx, `
INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
VALUES ($1, $2, $3, 'github.com/org/app', $4)`, fmt.Sprintf("orphan-ref-%d", i), scope, generation, referenceKey); err != nil {
			t.Fatalf("seed orphan relationship key: %v", err)
		}
	}
	after := factIDSet(runHoistedDeferredScopedQuery(t, ctx, db, params, scope, generation))
	if !slices.Equal(before, after) {
		t.Fatalf("deferred load with orphan keys = %v, want unchanged %v", after, before)
	}
}

// TestOrphanFactKeysCannotBeWrittenForAPrunedGenerationLive pins the write
// side of #7420: ingestion writes a fact's keys in the fact's own
// transaction, after the fact row, and fact_records keeps its foreign key to
// scope_generations. Once retention has deleted a generation, the fact
// insert for it fails and takes the key writes down with the transaction.
func TestOrphanFactKeysCannotBeWrittenForAPrunedGenerationLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	requireFactKeyForeignKeysDropped(t, ctx, database)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	envelopes := factKeyEnvelopes("keys-pruned", "keys-pruned-gone", 1)
	payload, _ := json.Marshal(envelopes[0].Payload)
	_, insertErr := tx.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'keys-pruned', 'keys-pruned-gone', 'content', $1, 'git', $1, now(), now(), $2::jsonb)`,
		envelopes[0].FactID, string(payload))
	_ = tx.Rollback()
	if insertErr == nil || !strings.Contains(insertErr.Error(), "foreign key") {
		t.Fatalf("fact insert into a deleted generation error = %v, want a foreign key violation", insertErr)
	}
	for _, table := range factKeyTables {
		if got := factKeyRowsFor(t, ctx, database, table, "keys-pruned", "keys-pruned-gone"); got != 0 {
			t.Errorf("%s rows for the deleted generation = %d, want 0", table, got)
		}
	}
}
