// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

const manifestLiveGitScope = "git-repo-checkout"

// manifestLiveGeneration seeds one git generation row.
type manifestLiveGeneration struct {
	id, status, commit, baseline string
	delta, activated             bool
	ingested                     time.Time
}

func manifestLiveSeedGitScope(t *testing.T, ctx context.Context, db *sql.DB, active string, generations ...manifestLiveGeneration) {
	t.Helper()
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`, manifestLiveGitScope, now); err != nil {
		t.Fatalf("seed git scope: %v", err)
	}
	for _, g := range generations {
		var activatedAt any
		if g.activated {
			activatedAt = g.ingested
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
  activated_at, source_commit_sha, is_delta, delta_baseline_commit_sha, delta_union_base_commit_sha, payload)
VALUES ($1, $2, 'synthetic', $3, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($8, ''), '{}'::jsonb)`,
			g.id, manifestLiveGitScope, g.ingested, g.status, activatedAt, g.commit, g.delta, g.baseline); err != nil {
			t.Fatalf("seed generation %s: %v", g.id, err)
		}
	}
	if active != "" {
		if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
			manifestLiveGitScope, active); err != nil {
			t.Fatalf("activate %s: %v", active, err)
		}
	}
}

// manifestLiveSeedFact inserts one fact row.
func manifestLiveSeedFact(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string, envelope facts.Envelope) {
	t.Helper()
	payload, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	source := envelope.SourceRef.SourceSystem
	if source == "" {
		source = "git"
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, '1.0.0', $6, $1, now(), now(), $7, $8::jsonb)`,
		envelope.FactID, scope, generation, envelope.FactKind, envelope.StableFactKey, source,
		envelope.IsTombstone, string(payload)); err != nil {
		t.Fatalf("seed fact %s: %v", envelope.FactID, err)
	}
}

// manifestLiveSeedMarker inserts the git scope's marker item for generation
// with status, stamped with the completion key when stamped is set.
func manifestLiveSeedMarker(t *testing.T, ctx context.Context, db *sql.DB, generation, status string, stamped bool) string {
	t.Helper()
	id := fanOutSeedItemAt(t, ctx, db, manifestLiveGitScope, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $2,
    payload = payload || jsonb_build_object('`+supplychaincore.PayloadKeyTrigger+`', '`+supplychaincore.TriggerManifestDependency+`')
            || CASE WHEN $3 THEN jsonb_build_object('`+supplychaincore.PayloadKeyManifestTriggerCompleted+`',
                   jsonb_build_object('at', now(), 'baseline_generation_id', '')) ELSE '{}'::jsonb END
WHERE work_item_id = $1`, id, status, stamped); err != nil {
		t.Fatalf("seed marker %s: %v", id, err)
	}
	return id
}

func manifestLiveDependency(factID, path, name, version string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, FactKind: "content_entity", StableFactKey: "entity:" + path + ":" + name,
		Payload: map[string]any{
			"entity_type": "Variable", "entity_name": name, "repo_id": "repository:r_checkout",
			"relative_path": path,
			"entity_metadata": map[string]any{
				"config_kind": "dependency", "package_manager": "npm", "value": version,
			},
		},
	}
}

func manifestLiveRepository(factID string, delta bool, changed ...string) facts.Envelope {
	payload := map[string]any{"repo_id": "repository:r_checkout", "graph_kind": "repository", "path": "/repos/checkout"}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = changed
		payload["delta_deleted_relative_paths"] = []string{}
	}
	return facts.Envelope{FactID: factID, FactKind: "repository", StableFactKey: "repository:r_checkout", Payload: payload}
}

func manifestLiveAffected(factID, ecosystem, name string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, FactKind: "vulnerability.affected_package", StableFactKey: factID,
		SourceRef: facts.Ref{SourceSystem: "vulnerability_intelligence"},
		Payload: map[string]any{
			"ecosystem": ecosystem, "package_name": name, "cve_id": "CVE-2026-0001",
			"purl": "pkg:" + ecosystem + "/" + name, "package_id": ecosystem + "://registry/" + name,
		},
	}
}

func factIDs(envelopes []facts.Envelope) []string {
	ids := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		ids = append(ids, envelope.FactID)
	}
	sort.Strings(ids)
	return ids
}

// TestFindPackageHolderCandidatesLive proves the package-keyed holder read
// returns only active, non-tombstoned affected-package rows in the asked
// ecosystems, outside the asking scope.
func TestFindPackageHolderCandidatesLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "vuln-intel://osv/a", "g2", "g1")
	fanOutSeedScope(t, ctx, db, "vuln-intel://osv/b", "g2", "g1")
	fanOutSeedScope(t, ctx, db, "vuln-intel://osv/c", "g1")
	fanOutSeedScope(t, ctx, db, manifestLiveGitScope, "g1")
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/a", fanOutGen("vuln-intel://osv/a", "g2"), manifestLiveAffected("ap-a", "npm", "lodash"))
	// Only the superseded generation of b names lodash.
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/b", fanOutGen("vuln-intel://osv/b", "g1"), manifestLiveAffected("ap-b-old", "npm", "lodash"))
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/c", fanOutGen("vuln-intel://osv/c", "g1"), manifestLiveAffected("ap-c", "pypi", "requests"))
	tomb := manifestLiveAffected("ap-c-tomb", "npm", "lodash")
	tomb.IsTombstone = true
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/c", fanOutGen("vuln-intel://osv/c", "g1"), tomb)
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, fanOutGen(manifestLiveGitScope, "g1"), manifestLiveAffected("ap-own", "npm", "lodash"))

	got, err := fanOutLiveStore(db).FindPackageHolderCandidates(ctx, manifestLiveGitScope, []string{"NPM"})
	if err != nil {
		t.Fatalf("FindPackageHolderCandidates() error = %v", err)
	}
	want := []supplychaincore.PackageHolderCandidate{{
		ScopeID: "vuln-intel://osv/a", Ecosystem: "npm", PackageName: "lodash",
		PURL: "pkg:npm/lodash", PackageID: "npm://registry/lodash",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %+v, want %+v", got, want)
	}
	if none, err := fanOutLiveStore(db).FindPackageHolderCandidates(ctx, manifestLiveGitScope, nil); err != nil || len(none) != 0 {
		t.Fatalf("empty ecosystems = %v, %v; want no rows and no error", none, err)
	}
}

// TestFindPackageHolderCandidatesMatchesRawEcosystemSpellingsLive is the
// #9874 F1 regression: sources store raw ecosystem spellings (a GitLab
// Gemnasium slug writes go, packagist, gem), and the handler asks with the
// canonical names. Every spelling NormalizeEcosystem accepts must reach the
// candidate set, or the prefilter under-matches the findings pass's join.
func TestFindPackageHolderCandidatesMatchesRawEcosystemSpellingsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	cases := []struct{ stored, asked string }{
		{"go", "gomod"},
		{"golang", "gomod"},
		{"gomod", "gomod"},
		{"packagist", "composer"},
		{"composer", "composer"},
		{"gem", "rubygems"},
		{"rubygems", "rubygems"},
		{"npm", "npm"},
		{"pypi", "pypi"},
		{"maven", "maven"},
		{"nuget", "nuget"},
		{" GO ", "gomod"},
	}
	for i, tc := range cases {
		scope := fmt.Sprintf("vuln-intel://gitlab/%02d", i)
		fanOutSeedScope(t, ctx, db, scope, "g1")
		manifestLiveSeedFact(t, ctx, db, scope, fanOutGen(scope, "g1"),
			manifestLiveAffected(fmt.Sprintf("ap-raw-%02d", i), tc.stored, "pkg"))
	}
	for i, tc := range cases {
		got, err := fanOutLiveStore(db).FindPackageHolderCandidates(ctx, manifestLiveGitScope, []string{tc.asked})
		if err != nil {
			t.Fatalf("FindPackageHolderCandidates(%q) error = %v", tc.asked, err)
		}
		want := fmt.Sprintf("vuln-intel://gitlab/%02d", i)
		found := false
		for _, candidate := range got {
			found = found || candidate.ScopeID == want
		}
		if !found {
			t.Fatalf("asking %q missed the row stored as %q (scope %s); got %+v", tc.asked, tc.stored, want, got)
		}
	}
}

// TestReadManifestDependencyGenerationLive proves the activation gate and the
// current and baseline views, including a stamped baseline delta overlaid on
// its base and a baseline delta whose base is gone.
func TestReadManifestDependencyGenerationLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	at := func(minute int) time.Time { return time.Date(2026, 11, 4, 0, minute, 0, 0, time.UTC) }
	manifestLiveSeedGitScope(t, ctx, db, "gen-3",
		manifestLiveGeneration{id: "gen-1", status: "superseded", commit: "c1", activated: true, ingested: at(1)},
		manifestLiveGeneration{id: "gen-2", status: "superseded", commit: "c2", baseline: "c1", delta: true, activated: true, ingested: at(2)},
		manifestLiveGeneration{id: "gen-3", status: "active", commit: "c3", activated: true, ingested: at(3)},
		manifestLiveGeneration{id: "gen-4", status: "pending", commit: "c4", ingested: at(4)},
	)
	seed := func(generation string, envelope facts.Envelope) {
		manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, generation, envelope)
	}
	manifestLiveSeedMarker(t, ctx, db, "gen-2", "succeeded", true)
	seed("gen-1", manifestLiveRepository("repo-1", false))
	seed("gen-1", manifestLiveDependency("g1-lodash", "package-lock.json", "lodash", "4.17.20"))
	seed("gen-1", manifestLiveDependency("g1-left-pad", "package-lock.json", "left-pad", "1.3.0"))
	seed("gen-2", manifestLiveRepository("repo-2", true, "README.md"))
	seed("gen-3", manifestLiveRepository("repo-3", false))
	seed("gen-3", manifestLiveDependency("g3-lodash", "package-lock.json", "lodash", "4.17.21"))
	removed := manifestLiveDependency("g3-tomb", "package-lock.json", "left-pad", "1.3.0")
	removed.IsTombstone = true
	seed("gen-3", removed)
	// A non-dependency entity must not be read.
	plain := manifestLiveDependency("g3-plain", "main.go", "timeout", "30")
	plain.Payload["entity_metadata"] = map[string]any{"config_kind": "constant"}
	seed("gen-3", plain)

	store := fanOutLiveStore(db)
	got, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-3")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration(gen-3) error = %v", err)
	}
	if got.Activation != supplychaincore.ManifestGenerationActive {
		t.Fatalf("Activation = %q, want active", got.Activation)
	}
	if ids := factIDs(got.Current.Rows); !reflect.DeepEqual(ids, []string{"g3-lodash", "g3-tomb", "repo-3"}) {
		t.Fatalf("current rows = %v", ids)
	}
	if !got.HasBaseline || got.Baseline.GenerationID != "gen-2" || got.Baseline.BaseMissing || len(got.Chain) != 0 {
		t.Fatalf("baseline = %q (has %v, base missing %v), chain %d; want stamped gen-2 with its base and no chain",
			got.Baseline.GenerationID, got.HasBaseline, got.Baseline.BaseMissing, len(got.Chain))
	}
	if ids := factIDs(got.Baseline.Rows); !reflect.DeepEqual(ids, []string{"repo-2"}) {
		t.Fatalf("prior rows = %v, want the delta's own rows", ids)
	}
	if ids := factIDs(got.Baseline.Base); !reflect.DeepEqual(ids, []string{"g1-left-pad", "g1-lodash", "repo-1"}) {
		t.Fatalf("prior base rows = %v, want gen-1's rows", ids)
	}

	pending, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-4")
	if err != nil || pending.Activation != supplychaincore.ManifestGenerationPending {
		t.Fatalf("gen-4 = %+v, %v; want pending", pending.Activation, err)
	}
	older, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-1")
	if err != nil || older.Activation != supplychaincore.ManifestGenerationSuperseded {
		t.Fatalf("gen-1 = %+v, %v; want superseded", older.Activation, err)
	}

	// Remove the base: the prior delta now reads as base-missing.
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE generation_id = 'gen-1'`); err != nil {
		t.Fatalf("delete base facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM scope_generations WHERE generation_id = 'gen-1'`); err != nil {
		t.Fatalf("delete base generation: %v", err)
	}
	missing, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-3")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration(base gone) error = %v", err)
	}
	if !missing.HasBaseline || !missing.Baseline.BaseMissing || len(missing.Baseline.Base) != 0 {
		t.Fatalf("base gone: HasBaseline=%v BaseMissing=%v base=%v", missing.HasBaseline, missing.Baseline.BaseMissing, factIDs(missing.Baseline.Base))
	}
}

// TestManifestTriggerReopensAdvisoryHolderLive is the end-to-end proof on a
// real queue: a marker intent for a git generation that bumped lodash reopens
// the succeeded supply_chain_impact item of the advisory scope holding lodash,
// flags the in-flight one for a rerun (the concurrency case against a running
// OSV pass), leaves the unrelated advisory scope's item alone, writes no
// finding, and changes nothing more when redelivered.
func TestManifestTriggerReopensAdvisoryHolderLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	at := func(minute int) time.Time { return time.Date(2026, 11, 4, 0, minute, 0, 0, time.UTC) }
	manifestLiveSeedGitScope(t, ctx, db, "gen-2",
		manifestLiveGeneration{id: "gen-1", status: "superseded", commit: "c1", activated: true, ingested: at(1)},
		manifestLiveGeneration{id: "gen-2", status: "active", commit: "c2", activated: true, ingested: at(2)},
	)
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-1", manifestLiveRepository("repo-1", false))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-1", manifestLiveDependency("g1-lodash", "package-lock.json", "lodash", "4.17.20"))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveRepository("repo-2", false))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveDependency("g2-lodash", "package-lock.json", "lodash", "4.17.21"))
	for _, scope := range []string{"vuln-intel://osv/lodash", "vuln-intel://osv/other"} {
		fanOutSeedScope(t, ctx, db, scope, "g1")
		id := fanOutSeedItem(t, ctx, db, scope, "g1")
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, id); err != nil {
			t.Fatalf("mark succeeded: %v", err)
		}
	}
	// An OSV pass for lodash already in flight (running, it may have read the
	// old pin): the marker must flag it for one rerun, not skip it.
	fanOutSeedScope(t, ctx, db, "vuln-intel://osv/lodash-running", "g1")
	runningID := fanOutSeedItem(t, ctx, db, "vuln-intel://osv/lodash-running", "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'running', lease_owner = 'other-worker',
  claim_until = now() + interval '5 minutes' WHERE work_item_id = $1`, runningID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/lodash-running", fanOutGen("vuln-intel://osv/lodash-running", "g1"), manifestLiveAffected("ap-lodash-running", "npm", "lodash"))
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/lodash", fanOutGen("vuln-intel://osv/lodash", "g1"), manifestLiveAffected("ap-lodash", "npm", "lodash"))
	manifestLiveSeedFact(t, ctx, db, "vuln-intel://osv/other", fanOutGen("vuln-intel://osv/other", "g1"), manifestLiveAffected("ap-other", "npm", "express"))

	handler := supplychaincore.SupplyChainImpactHandler{
		FencingTokenIssuer: manifestLiveNoToken{t: t},
		FactLoader:         manifestLiveNoLoad{t: t},
		Writer:             manifestLiveNoWrite{t: t},
		FanOut:             fanOutLiveStore(db),
	}
	// The marker item is claimed like production does, so the pass holds the
	// lease its completion stamp is fenced on.
	markerID := manifestLiveSeedMarker(t, ctx, db, "gen-2", "pending", false)
	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != markerID {
		t.Fatalf("Claim() = %q, %v, %v; want the marker item %q", intent.IntentID, ok, err, markerID)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	status := func(scope string) string {
		var s string
		if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`,
			fanOutItemID(scope, fanOutGen(scope, "g1"))).Scan(&s); err != nil {
			t.Fatalf("read status %s: %v", scope, err)
		}
		return s
	}
	if got := status("vuln-intel://osv/lodash"); got != "pending" {
		t.Fatalf("lodash holder status = %q, want pending (reopened)", got)
	}
	if got := status("vuln-intel://osv/other"); got != "succeeded" {
		t.Fatalf("unrelated holder status = %q, want succeeded (untouched)", got)
	}
	var flagged bool
	if err := db.QueryRowContext(ctx, `SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`,
		runningID).Scan(&flagged); err != nil {
		t.Fatalf("read replay flag: %v", err)
	}
	if !flagged {
		t.Fatal("in-flight lodash holder was not flagged for a rerun")
	}
	// Re-delivery is idempotent: the reopened holder is already pending and the
	// running one already flagged, so a second marker pass changes nothing.
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("redelivered Handle() error = %v", err)
	}
	if got := status("vuln-intel://osv/lodash"); got != "pending" {
		t.Fatalf("lodash holder after redelivery = %q, want pending", got)
	}
}

type manifestLiveNoToken struct{ t *testing.T }

func (n manifestLiveNoToken) NextSupplyChainImpactFencingToken(context.Context) (int64, error) {
	n.t.Fatal("marker pass issued a fencing token")
	return 0, nil
}

type manifestLiveNoLoad struct{ t *testing.T }

func (n manifestLiveNoLoad) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	n.t.Fatal("marker pass loaded evidence")
	return nil, nil
}

type manifestLiveNoWrite struct{ t *testing.T }

func (n manifestLiveNoWrite) WriteSupplyChainImpactFindings(context.Context, supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	n.t.Fatal("marker pass wrote findings")
	return supplychaincore.SupplyChainImpactWriteResult{}, nil
}
