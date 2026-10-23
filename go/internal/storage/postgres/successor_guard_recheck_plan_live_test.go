// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8750 disposable PostgreSQL 18 measurement of the destructive-successor guard's in-tx recheck reads (the candidate read and the active read) with EXPLAIN (ANALYZE, BUFFERS) on a proof scope seeded with 3,000 generations beside 3,000 other-scope generations; reuses the #7978 bootstrapped proof schema; plans and timings are logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// seedRecheckPlanScopes gives the proof scope 3,000 older superseded
// generations with succeeded projector rows, and adds 200 other repository
// scopes of 15 generations each (one active), so both recheck reads run
// against a large scope among many.
func (p *reprojectProof) seedRecheckPlanScopes() {
	p.t.Helper()
	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at, ingested_at, status, activated_at)
SELECT 'old-' || g, $1, 'snapshot', 'hint-old-' || g, now() - interval '30 days' + g * interval '1 second',
       now() - interval '30 days' + g * interval '1 second', 'superseded', now() - interval '30 days'
FROM generate_series(1, 3000) AS g`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, visible_at, payload, created_at, updated_at)
SELECT 'projector_' || $1 || '_old-' || g, $1, 'old-' || g, 'projector', 'source_local', 'succeeded', 1, now(), '{}'::jsonb, now(), now()
FROM generate_series(1, 3000) AS g`,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
SELECT 'other-' || s, 'repository', 'git', 'other/' || s, 'git', 'other/' || s, now(), now(), 'active', 'other-' || s || '-15'
FROM generate_series(1, 200) AS s`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at, ingested_at, status, activated_at)
SELECT 'other-' || s || '-' || g, 'other-' || s, 'snapshot', 'hint-' || g, now() - (15 - g) * interval '1 hour',
       now() - (15 - g) * interval '1 hour', CASE WHEN g = 15 THEN 'active' ELSE 'superseded' END, now() - interval '1 day'
FROM generate_series(1, 200) AS s, generate_series(1, 15) AS g`,
		`ANALYZE scope_generations`,
		`ANALYZE fact_work_items`,
	} {
		args := []any{}
		if strings.Contains(statement, "$1") {
			args = append(args, p.scope.ScopeID)
		}
		if _, err := p.db.ExecContext(ctx, statement, args...); err != nil {
			p.t.Fatalf("seed recheck plan scopes: %v\n%s", err, statement)
		}
	}
}

var explainExecutionTime = regexp.MustCompile(`Execution Time: ([0-9.]+) ms`)

// explainRecheckRead runs EXPLAIN (ANALYZE, BUFFERS) of query runs times and
// logs the execution times and the last plan.
func (p *reprojectProof) explainRecheckRead(name, query string, runs int) {
	p.t.Helper()
	times := make([]float64, 0, runs)
	var plan []string
	for range runs {
		rows, err := p.db.QueryContext(context.Background(), "EXPLAIN (ANALYZE, BUFFERS) "+query, p.scope.ScopeID)
		if err != nil {
			p.t.Fatalf("explain %s: %v", name, err)
		}
		plan = plan[:0]
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				p.t.Fatalf("explain %s scan: %v", name, err)
			}
			plan = append(plan, line)
		}
		if err := rows.Close(); err != nil {
			p.t.Fatalf("explain %s close: %v", name, err)
		}
		for _, line := range plan {
			if match := explainExecutionTime.FindStringSubmatch(line); match != nil {
				ms, err := strconv.ParseFloat(match[1], 64)
				if err != nil {
					p.t.Fatalf("parse %s execution time %q: %v", name, match[1], err)
				}
				times = append(times, ms)
			}
		}
	}
	slices.Sort(times)
	if len(times) != runs {
		p.t.Fatalf("%s: %d execution times from %d runs", name, len(times), runs)
	}
	p.t.Logf("%s execution ms over %d runs: min %.3f median %.3f max %.3f\n%s",
		name, runs, times[0], times[len(times)/2], times[len(times)-1], strings.Join(plan, "\n"))
}

// TestGuardRecheckReadPlansLive measures the two recheck reads the guard
// adds inside the commit transaction. The candidate read runs on every
// guarded commit; the active read only when the candidate is pending.
func TestGuardRecheckReadPlansLive(t *testing.T) {
	p := openReprojectProof(t)
	p.seedLargeFullPrior(50, 0)
	p.seedRecheckPlanScopes()
	var generations int
	if err := p.db.QueryRowContext(context.Background(), `SELECT count(*) FROM scope_generations`).Scan(&generations); err != nil {
		t.Fatalf("count generations: %v", err)
	}
	t.Logf("scope_generations rows: %d (proof scope 3,001)", generations)

	p.explainRecheckRead("candidate read, candidate active", unchangedGenerationCandidateQuery, 9)
	p.commitGuardSizing(p.generation("gen-p", "hint-p", "sha-p", "interval_elapsed"), guardSizingFiles("f", 50), 50, nil, nil)
	p.explainRecheckRead("candidate read, candidate pending", unchangedGenerationCandidateQuery, 9)
	p.explainRecheckRead("active read", successorGuardActiveGenerationQuery, 9)

	// Worst case for the candidate read's filtered index walk: 3,000 newer
	// failed generations sort ahead of the pending candidate.
	if _, err := p.db.ExecContext(context.Background(), `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at, ingested_at, status)
SELECT 'failed-' || g, $1, 'snapshot', 'hint-failed-' || g, now() + g * interval '1 second', now() + g * interval '1 second', 'failed'
FROM generate_series(1, 3000) AS g`, p.scope.ScopeID); err != nil {
		t.Fatalf("seed newer failed generations: %v", err)
	}
	if _, err := p.db.ExecContext(context.Background(), `ANALYZE scope_generations`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	p.explainRecheckRead("candidate read, 3,000 newer failed rows ahead", unchangedGenerationCandidateQuery, 9)
}
