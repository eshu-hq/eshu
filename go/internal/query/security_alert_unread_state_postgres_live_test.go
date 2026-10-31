// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8943 disposable PostgreSQL proof of the security-alert unread-repository read on the bootstrapped schema: the read follows ingestion_scopes.active_generation_id to the generation's read state and returns unread rows of the requested repositories only, ignores a superseded generation's rows, reports a target with no read state as nothing unread, returns the distinct reasons without any child row on an unanchored read, reports an active retirement generation (#9019) as retired and an older one under a newer collection as not, and plans the child read as primary-key index probes on a seeded set of 50 targets with 4 generations and 2,000 unread repositories each; isolated bootstrapped schema per test; plan timing is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// readState seeds a generation of scope with its read state and unread rows,
// and optionally makes it the scope's active generation.
func (f *collectionStateLiveFixture) readState(scope, generation string, active bool, reasons []string, unread map[string]string) {
	f.t.Helper()
	if reasons == nil {
		reasons = []string{}
	}
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'security_alert', 'security_alert', $1, 'security_alert', $1, $2, $2, 'active')
ON CONFLICT (scope_id) DO NOTHING`, scope, f.asOf)
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, $4)`, generation, scope, f.asOf, map[bool]string{true: "active", false: "superseded"}[active])
	f.exec(`INSERT INTO security_alert_target_read_states
    (generation_id, scope_id, org_read_truncated, repositories_total, unread_total, unread_reasons, observed_at)
VALUES ($1, $2, TRUE, $3, $4, $5, $6)`, generation, scope, len(unread)+5, len(unread), array.Of(reasons), f.asOf)
	for repository, reason := range unread {
		f.exec(`INSERT INTO security_alert_unread_repositories (generation_id, repository, reason) VALUES ($1, $2, $3)`,
			generation, repository, reason)
	}
	if active {
		f.exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, generation)
	}
}

func TestSecurityAlertUnreadRepositoriesLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t)
	// t-org: the active generation left two repositories unread; an older
	// superseded generation left a third that must not be reported.
	f.readState("t-org", "gen-org-old", false, []string{"org_repository_read_failed"},
		map[string]string{"acme/old-only": "org_repository_read_failed"})
	f.readState("t-org", "gen-org-active", true,
		[]string{"org_repository_fanout_cap_reached", "org_repository_read_rate_limited"},
		map[string]string{
			"acme/capped":  "org_repository_fanout_cap_reached",
			"acme/limited": "org_repository_read_rate_limited",
		})
	// t-clean: a read state with nothing unread.
	f.readState("t-clean", "gen-clean", true, nil, map[string]string{})
	// t-legacy: a committed generation written before the carrier.
	f.generation("t-legacy", "active")
	f.exec(`UPDATE ingestion_scopes SET active_generation_id = 't-legacy-active' WHERE scope_id = 't-legacy'`)

	scopes := []string{"t-clean", "t-legacy", "t-org"}
	states, err := f.store().SecurityAlertTargetStates(f.ctx, scopes, []string{"acme/capped", "acme/fine", "acme/old-only"})
	if err != nil {
		t.Fatalf("SecurityAlertTargetStates() error = %v", err)
	}
	byScope := map[string]int{}
	for i, state := range states {
		byScope[state.ScopeID] = i
	}
	org := states[byScope["t-org"]]
	if org.UnreadTotal != 2 || !reflect.DeepEqual(org.UnreadReasons, []string{"org_repository_fanout_cap_reached", "org_repository_read_rate_limited"}) {
		t.Errorf("t-org total/reasons = %d %v, want 2 and both reasons", org.UnreadTotal, org.UnreadReasons)
	}
	if want := map[string]string{"acme/capped": "org_repository_fanout_cap_reached"}; !reflect.DeepEqual(org.UnreadRequested, want) {
		t.Errorf("t-org requested unread = %v, want %v (a read repository and a superseded generation's repository are absent)", org.UnreadRequested, want)
	}
	for _, scope := range []string{"t-clean", "t-legacy"} {
		state := states[byScope[scope]]
		if state.UnreadTotal != 0 || len(state.UnreadReasons) != 0 || len(state.UnreadRequested) != 0 {
			t.Errorf("%s reports unread state %+v, want none", scope, state)
		}
	}
	if !states[byScope["t-legacy"]].CommittedGeneration {
		t.Error("t-legacy lost its committed generation")
	}

	// An unanchored read passes no slugs: totals and reasons only.
	states, err = f.store().SecurityAlertTargetStates(f.ctx, []string{"t-org"}, nil)
	if err != nil {
		t.Fatalf("unanchored SecurityAlertTargetStates() error = %v", err)
	}
	if states[0].UnreadTotal != 2 || len(states[0].UnreadRequested) != 0 {
		t.Errorf("unanchored t-org = %+v, want total 2 and no requested rows", states[0])
	}

	// #9019: an active retirement generation reads as Retired; an older
	// retirement under a newer collection does not, and t-org (a collection)
	// is not retired either.
	f.readState("t-retired", "gen-retired", true, nil, map[string]string{})
	f.readState("t-recollected", "gen-recollected-old", false, nil, map[string]string{})
	f.readState("t-recollected", "gen-recollected", true, nil, map[string]string{})
	f.exec(`UPDATE scope_generations SET freshness_hint = $1 WHERE generation_id IN ('gen-retired', 'gen-recollected-old')`,
		shard.RetirementFreshnessHint)
	f.exec(`UPDATE scope_generations SET freshness_hint = 'hint-collected' WHERE generation_id = 'gen-recollected'`)
	states, err = f.store().SecurityAlertTargetStates(f.ctx, []string{"t-org", "t-recollected", "t-retired"}, []string{"acme/capped"})
	if err != nil {
		t.Fatalf("retired SecurityAlertTargetStates() error = %v", err)
	}
	for _, state := range states {
		if want := state.ScopeID == "t-retired"; state.Retired != want || !state.CommittedGeneration {
			t.Errorf("%s Retired = %v committed = %v, want retired %v and committed", state.ScopeID, state.Retired, state.CommittedGeneration, want)
		}
	}
}

// TestSecurityAlertUnreadRepositoriesPlanLive seeds 50 targets with 4
// generations each and 2,000 unread repositories per generation, then asserts
// the child read is a primary-key probe on (generation_id, repository) and logs
// the plan's execution time and buffers.
func TestSecurityAlertUnreadRepositoriesPlanLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t)
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
SELECT 'target-' || t, 'security_alert', 'security_alert', 'target-' || t, 'security_alert', 'p', $1, $1, 'active'
FROM generate_series(1, 50) AS t`, f.asOf)
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'gen-' || t || '-' || g, 'target-' || t, 'snapshot', $1, $1, CASE WHEN g = 4 THEN 'active' ELSE 'superseded' END
FROM generate_series(1, 50) AS t, generate_series(1, 4) AS g`, f.asOf)
	f.exec(`INSERT INTO security_alert_target_read_states
    (generation_id, scope_id, org_read_truncated, repositories_total, unread_total, unread_reasons, observed_at)
SELECT 'gen-' || t || '-' || g, 'target-' || t, TRUE, 2005, 2000, ARRAY['org_repository_fanout_cap_reached'], $1
FROM generate_series(1, 50) AS t, generate_series(1, 4) AS g`, f.asOf)
	f.exec(`INSERT INTO security_alert_unread_repositories (generation_id, repository, reason)
SELECT 'gen-' || t || '-' || g, 'acme' || t || '/repo-' || r, 'org_repository_fanout_cap_reached'
FROM generate_series(1, 50) AS t, generate_series(1, 4) AS g, generate_series(1, 2000) AS r`)
	f.exec(`UPDATE ingestion_scopes SET active_generation_id = 'gen-' || substr(scope_id, 8) || '-4'`)
	f.exec(`VACUUM (ANALYZE) ingestion_scopes, scope_generations, security_alert_target_read_states, security_alert_unread_repositories`)

	scopeIDs := make([]string, 50)
	for i := range scopeIDs {
		scopeIDs[i] = fmt.Sprintf("target-%d", i+1)
	}
	started := time.Now()
	states, err := f.store().SecurityAlertTargetStates(f.ctx, scopeIDs[:1], []string{"acme1/repo-7", "acme1/repo-2001"})
	if err != nil {
		t.Fatalf("SecurityAlertTargetStates() error = %v", err)
	}
	t.Logf("one anchored target read took %s", time.Since(started))
	if len(states) != 1 || len(states[0].UnreadRequested) != 1 {
		t.Fatalf("states = %+v, want the one unread requested repository", states)
	}
	rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+securityAlertTargetStatesQuery,
		array.Of(scopeIDs), array.Of([]string{"acme7/repo-7"}), shard.RetirementFreshnessHint)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	text := plan.String()
	t.Logf("EXPLAIN (ANALYZE, BUFFERS), 50 targets x 4 generations x 2,000 unread repositories, one requested slug:\n%s", text)
	if !strings.Contains(text, "security_alert_unread_repositories_pkey") {
		t.Fatalf("unread read does not use the primary key of security_alert_unread_repositories:\n%s", text)
	}
	if strings.Contains(text, "Seq Scan on security_alert_unread_repositories") {
		t.Fatalf("unread read scans security_alert_unread_repositories sequentially:\n%s", text)
	}
}
