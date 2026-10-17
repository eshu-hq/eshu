// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8243 disposable PostgreSQL 18 proof of the narrowed manifest-less dirty rule: every lifecycle fixture that can still need a producer (pending, uncovered writer, the supersede-then-delta hole, rows without a marked full) stays in the real loader's producer set on all three manifest reads, only covered superseded or failed generations leave it, and the rule agrees with UncoveredProjectionWriters; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// dirtyRuleBase is the fixed clock every #8243 lifecycle fixture is placed on.
var dirtyRuleBase = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

// dirtyRuleHours returns a pointer to dirtyRuleBase shifted by hours, or nil
// for a NULL column.
func dirtyRuleHours(hours float64) *time.Time {
	at := dirtyRuleBase.Add(time.Duration(hours * float64(time.Hour)))
	return &at
}

// dirtyRuleGeneration is one scope_generations row of a lifecycle fixture.
type dirtyRuleGeneration struct {
	suffix    string
	status    string
	isDelta   bool
	ingested  float64
	activated *time.Time
	marker    *time.Time
}

// dirtyRuleFixture is one manifest-less repository scope with the generation
// history that decides whether the manifest reads must keep it dirty.
type dirtyRuleFixture struct {
	name   string
	active string
	rows   []dirtyRuleGeneration
	dirty  bool
}

// dirtyRuleFixtures covers every lifecycle the narrowed rule distinguishes
// (#8243). dirty is what the manifest reads must return. A scope that could
// still need a producer stays dirty; only a superseded or failed generation
// whose write start is at or before the newest activated full's write start
// is skipped, and a full without a marker covers nothing.
func dirtyRuleFixtures() []dirtyRuleFixture {
	full := func(suffix, status string, ingested float64, marker *time.Time) dirtyRuleGeneration {
		return dirtyRuleGeneration{suffix, status, false, ingested, dirtyRuleHours(ingested), marker}
	}
	return []dirtyRuleFixture{
		{"pending-writer", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"p", "pending", true, 120, nil, dirtyRuleHours(120)},
		}, true},
		{"uncovered-writer", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"s", "superseded", true, 48, nil, dirtyRuleHours(48)},
		}, true},
		{"legacy-row-later-marked-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"l", "superseded", true, -360, nil, nil},
		}, false},
		{"legacy-row-older-than-unmarked-full", "a", []dirtyRuleGeneration{
			full("a", "active", -168, nil),
			{"l", "superseded", true, -360, nil, nil},
		}, true},
		{"legacy-row-newer-than-unmarked-full", "a", []dirtyRuleGeneration{
			full("a", "active", -168, nil),
			{"l", "superseded", true, -120, nil, nil},
		}, true},
		{"no-activated-full", "a", []dirtyRuleGeneration{
			{"a", "active", true, 0, dirtyRuleHours(0), dirtyRuleHours(0)},
			{"l", "superseded", true, -360, nil, nil},
		}, true},
		// The supersede-then-delta hole: G2 deleted the manifest, Ack refused
		// it (superseded, never activated), and G3 never touched the path.
		{"hole", "g3", []dirtyRuleGeneration{
			{"g1", "superseded", false, -48, dirtyRuleHours(-48), dirtyRuleHours(-48)},
			{"g2", "superseded", true, 0, nil, dirtyRuleHours(0)},
			{"g3", "active", true, 48, dirtyRuleHours(72), dirtyRuleHours(48)},
		}, true},
		// A refused full writer: a non-delta generation that wrote after the
		// active full and never activated. It is not the newest activated
		// full (activated_at is NULL), so the active full stays the bound and
		// the writer stays dirty. A last_full lookup that dropped
		// "activated_at IS NOT NULL" would pick the writer itself and clear it.
		{"refused-full-writer", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"r", "superseded", false, 24, nil, dirtyRuleHours(24)},
		}, true},
		{"marked-writer-below-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"s", "superseded", true, -72, nil, dirtyRuleHours(-72)},
		}, false},
		{"marked-writer-equal-to-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"s", "superseded", true, -1, nil, dirtyRuleHours(0)},
		}, false},
		{"marked-writer-unmarked-full", "a", []dirtyRuleGeneration{
			full("a", "active", -168, nil),
			{"s", "superseded", true, -240, nil, dirtyRuleHours(-240)},
		}, true},
		{"newest-full-unmarked-older-full-marked", "a", []dirtyRuleGeneration{
			full("old", "superseded", -168, dirtyRuleHours(-168)),
			full("a", "active", 0, nil),
			{"s", "superseded", true, -200, nil, dirtyRuleHours(-200)},
		}, true},
		{"newest-full-superseded-but-activated", "d", []dirtyRuleGeneration{
			full("f", "superseded", 24, dirtyRuleHours(24)),
			{"d", "active", true, 48, dirtyRuleHours(48), dirtyRuleHours(48)},
			{"s", "superseded", true, 0, nil, dirtyRuleHours(0)},
		}, false},
		{"failed-unmarked-no-marked-full", "a", []dirtyRuleGeneration{
			full("a", "active", -168, nil),
			{"f", "failed", true, -24, nil, nil},
		}, true},
		{"failed-unmarked-marked-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"f", "failed", true, -24, nil, nil},
		}, false},
		{"failed-marked-above-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"f", "failed", true, 24, nil, dirtyRuleHours(24)},
		}, true},
		{"completed-unactivated", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"c", "completed", true, 24, nil, nil},
		}, true},
		{"active-without-activated-at", "a", []dirtyRuleGeneration{
			{"a", "active", false, 0, nil, dirtyRuleHours(0)},
		}, true},
		{"clean-history", "a", []dirtyRuleGeneration{
			full("old", "superseded", -168, dirtyRuleHours(-168)),
			full("a", "active", 0, dirtyRuleHours(0)),
		}, false},
		{"pending-unmarked", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"p", "pending", true, 24, nil, nil},
		}, true},
		// The hole healed: a later full (marked after G2's write) activated
		// and rewrote every content row, so G2 can no longer hide a manifest.
		{"hole-healed", "g4", []dirtyRuleGeneration{
			{"g1", "superseded", false, -48, dirtyRuleHours(-48), dirtyRuleHours(-48)},
			{"g2", "superseded", true, 0, nil, dirtyRuleHours(0)},
			{"g3", "superseded", true, 48, dirtyRuleHours(72), dirtyRuleHours(48)},
			full("g4", "active", 96, dirtyRuleHours(96)),
		}, false},
		// Divergence 4a from the rejected arbiter c2 rule: a claimed NULL-marker
		// row superseded 30 minutes before a marked full started writing.
		// Rule M: clean, because a marked full was written in the marker era
		// (post-149 code sets the marker before its first write), so a
		// NULL-marker row either never wrote or wrote before that full. The
		// rejected c2 (full write start > superseded_at + 1 h) would keep it
		// dirty.
		{"divergence-4a-claimed-unmarked-row-before-marked-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"b", "superseded", true, -2, nil, nil},
		}, false},
		// Divergence 4b: a claimed failed NULL-marker row with a marked newest
		// full. Rule M: clean, by the same era argument. The rejected c2 has
		// no superseded_at to compare and would keep it dirty.
		{"divergence-4b-claimed-failed-unmarked-row-marked-full", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"f", "failed", true, -24, nil, nil},
		}, false},
		// Fixture 914 from stage 1: a NULL-marker row claimed after its
		// supersede (a pre-#7266 late claim) and after the marked newest full.
		// Rule M: clean. A row is dropped only when the newest activated full
		// carries a marker, i.e. was written in the marker era (after
		// 2026-10-01 on QA, after #7266), so a late claim that wrote would
		// have set the marker first; a NULL marker means it never wrote. With
		// an unmarked newest full the same row stays dirty
		// (legacy-row-older-than-unmarked-full). The uncorrected arbiter c2
		// would also clear it; the corrected c2 (last_attempt_at <=
		// superseded_at) would keep it dirty.
		{"late-claim-914", "a", []dirtyRuleGeneration{
			full("a", "active", 0, dirtyRuleHours(0)),
			{"b", "superseded", true, -96, nil, nil},
		}, false},
	}
}

// dirtyRuleClaim is a projector work row and supersede time for one fixture
// generation. Rule M reads neither; they make the divergence fixtures the
// concrete states the rejected rules decide on.
type dirtyRuleClaim struct {
	supersededAt *time.Time
	claimedAt    *time.Time
}

// dirtyRuleClaims maps fixture name to generation suffix to its claim.
var dirtyRuleClaims = map[string]map[string]dirtyRuleClaim{
	"divergence-4a-claimed-unmarked-row-before-marked-full": {"b": {dirtyRuleHours(-0.5), dirtyRuleHours(-0.7)}},
	"divergence-4b-claimed-failed-unmarked-row-marked-full": {"f": {nil, dirtyRuleHours(-23)}},
	"late-claim-914": {"b": {dirtyRuleHours(-48), dirtyRuleHours(24)}},
}

// dirtyRuleScopeID names a fixture's scope.
func dirtyRuleScopeID(name string) string { return "scope:dirty-" + name }

// seedDirtyRuleFixtures writes every fixture as a manifest-less repository
// scope. Each dirty scope's active generation carries one file fact whose
// function publishes package_id fx-<name>, export run, so a load can prove
// which scopes the anchored scan visited; a covered scope's fact carries no
// package_id.
func seedDirtyRuleFixtures(t *testing.T, ctx context.Context, database *sql.DB) []dirtyRuleFixture {
	t.Helper()
	fixtures := dirtyRuleFixtures()
	for i, fixture := range fixtures {
		scopeID := dirtyRuleScopeID(fixture.name)
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'repository', 'git', 'repository:' || $1, 'git', $1, $2, $2, 'active', $1 || '-' || $3)`,
			scopeID, dirtyRuleBase, fixture.active); err != nil {
			t.Fatalf("insert scope %q: %v", scopeID, err)
		}
		for _, row := range fixture.rows {
			ingested := dirtyRuleHours(row.ingested)
			if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    is_delta, activated_at, projection_write_started_at
) VALUES ($1 || '-' || $2, $1, 'snapshot', $3, $3, $4, $5, $6, $7)`,
				scopeID, row.suffix, *ingested, row.status, row.isDelta, row.activated, row.marker); err != nil {
				t.Fatalf("insert generation %s-%s: %v", scopeID, row.suffix, err)
			}
			claim, ok := dirtyRuleClaims[fixture.name][row.suffix]
			if !ok {
				continue
			}
			generationID := scopeID + "-" + row.suffix
			if _, err := database.ExecContext(ctx, `UPDATE scope_generations SET superseded_at = $2 WHERE generation_id = $1`,
				generationID, claim.supersededAt); err != nil {
				t.Fatalf("stamp superseded_at %s: %v", generationID, err)
			}
			if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, last_attempt_at, created_at, updated_at
) VALUES ('projector_' || $1 || '_' || $2, $1, $2, 'projector', 'source_local', 'superseded', 1, $3, $4, $4)`,
				scopeID, generationID, claim.claimedAt, *ingested); err != nil {
				t.Fatalf("insert work row %s: %v", generationID, err)
			}
		}
		observedAt := dirtyRuleBase.Add(time.Duration(i) * time.Second)
		if !fixture.dirty {
			// A covered scope's active truth holds no manifest, so the parser
			// stamps no package_id on its definitions.
			seedActiveCodeCallSymbolFact(t, ctx, database, "fact-"+fixture.name, scopeID,
				scopeID+"-"+fixture.active, "src/index.js", "scip-covered "+fixture.name, observedAt)
			continue
		}
		seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-"+fixture.name, scopeID,
			scopeID+"-"+fixture.active, "src/index.js", "fx-"+fixture.name, "run", observedAt)
	}
	return fixtures
}

// dirtyRuleProducerScopes runs the real loader for one key through queryer
// and returns the producer scopes recorder saw bound as $5 of the anchored
// scan, or nil when the loader issued no scan.
func dirtyRuleProducerScopes(t *testing.T, ctx context.Context, recorder *recordingCodeCallSymbolQueryer, queryer db.ExecQueryer, key string) []string {
	t.Helper()
	recorder.queries, recorder.args = nil, nil
	if _, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{key}); err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(%q) error = %v", key, err)
	}
	for i, query := range recorder.queries {
		if query == listAnchoredActiveCodeCallSymbolDefinitionFactsQuery {
			scopes, ok := recorder.args[i][4].([]string)
			if !ok {
				t.Fatalf("anchored scan $5 = %T, want []string", recorder.args[i][4])
			}
			return scopes
		}
	}
	return nil
}

// dirtyRuleKeys are one unpublished key per manifest read, so each load's
// producer set is exactly that read's dirty scopes.
var dirtyRuleKeys = map[string]string{
	"package.json":  "package:@fixture/unpublished#run",
	"composer.json": "package:fixture/unpublished#run",
	"go.mod":        "scip-go gomod example.com/unpublished pkg/Run().",
}

// TestReducerContentionGateManifestLessDirtyRuleKeepsEveryScopeThatCanNeedAProducer
// proves the narrowed manifest-less rule (#8243) on real Postgres through the
// real loader, for all three manifest reads: every fixture that can still
// need a producer (pending, uncovered writer, the supersede-then-delta hole,
// legacy rows without a marked full, failed or unknown-status rows that are
// not covered) is in the producer set, and only covered superseded or failed
// generations leave it.
func TestReducerContentionGateManifestLessDirtyRuleKeepsEveryScopeThatCanNeedAProducer(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	fixtures := seedDirtyRuleFixtures(t, ctx, database)
	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}

	for manifest, key := range dirtyRuleKeys {
		scopes := dirtyRuleProducerScopes(t, ctx, queryer, queryer, key)
		for _, fixture := range fixtures {
			got := slices.Contains(scopes, dirtyRuleScopeID(fixture.name))
			if got != fixture.dirty {
				t.Errorf("%s read: fixture %q dirty = %v, want %v", manifest, fixture.name, got, fixture.dirty)
			}
		}
	}
}

// TestReducerContentionGateManifestLessDirtyRuleMatchesUncoveredProjectionWriters
// proves the narrowed rule agrees with UncoveredProjectionWriters, the
// definition the git collector's graph_dirty reconcile and generation
// retention use (#7389, #7472): for every fixture whose only never-activated
// rows are superseded or failed with a marker, the scope is dirty exactly
// when the probe reports an uncovered writer.
func TestReducerContentionGateManifestLessDirtyRuleMatchesUncoveredProjectionWriters(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	fixtures := seedDirtyRuleFixtures(t, ctx, database)
	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	scopes := dirtyRuleProducerScopes(t, ctx, queryer, queryer, dirtyRuleKeys["package.json"])
	store := NewIngestionStore(SQLDB{DB: database})

	compared := 0
	for _, fixture := range fixtures {
		markedOnly, unactivated := true, 0
		for _, row := range fixture.rows {
			if row.activated != nil {
				continue
			}
			unactivated++
			if row.marker == nil || (row.status != "superseded" && row.status != "failed") {
				markedOnly = false
			}
		}
		if !markedOnly || unactivated == 0 {
			continue
		}
		writers, err := store.UncoveredProjectionWriters(ctx, dirtyRuleScopeID(fixture.name))
		if err != nil {
			t.Fatalf("UncoveredProjectionWriters(%q) error = %v", fixture.name, err)
		}
		compared++
		if got, want := slices.Contains(scopes, dirtyRuleScopeID(fixture.name)), len(writers) > 0; got != want {
			t.Errorf("fixture %q: manifest read dirty = %v, UncoveredProjectionWriters non-empty = %v", fixture.name, got, want)
		}
	}
	if compared < 6 {
		t.Fatalf("compared %d marker-only fixtures, want at least 6", compared)
	}
}
