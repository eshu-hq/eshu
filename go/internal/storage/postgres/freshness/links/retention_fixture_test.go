// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// retentionLedgerTables are the ledger tables generation retention prunes
// (#7127 ruling 2.8). changed_since_key_state and changed_since_scope_cursor
// describe the active generation and are never pruned.
var retentionLedgerTables = []string{
	"changed_since_activations",
	"changed_since_links",
	"changed_since_link_deltas",
	"changed_since_link_bucket_counts",
}

// prunePolicy prunes every superseded generation older than an hour.
func prunePolicy(rowLimit int) postgres.GenerationRetentionPolicy {
	return postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            rowLimit,
		PolicyScope:              "global",
		PolicyRevision:           "7127-pr3d",
	}
}

// ledgerSnapshot is every row of the six ledger tables as
// "table|scope|generation|prior|rest", sorted. Generation-less tables carry
// empty generation fields.
func (l *ledgerDB) ledgerSnapshot(t *testing.T) []string {
	t.Helper()
	return ledgerSnapshotOn(t, l.ctx, l.raw)
}

// rowQueryer is a *sql.DB, *sql.Conn or *sql.Tx.
type rowQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ledgerSnapshotOn is ledgerSnapshot read through q, so a test can read the
// ledger inside an open transaction.
func ledgerSnapshotOn(t *testing.T, ctx context.Context, q rowQueryer) []string {
	t.Helper()
	var out []string
	for _, query := range []string{
		`SELECT 'changed_since_activations|' || scope_id || '|' || generation_id || '|' || COALESCE(prior_generation_id, '')
		   || '|' || activation_seq || '|' || source FROM changed_since_activations`,
		`SELECT 'changed_since_links|' || scope_id || '|' || generation_id || '|' || prior_generation_id || '|' || link_kind
		   || '|' || delta_rows || '|' || files_keys || '|' || content_entities_keys || '|' || facts_keys
		 FROM changed_since_links`,
		`SELECT 'changed_since_link_deltas|' || scope_id || '|' || generation_id || '|' || prior_generation_id || '|'
		   || fact_category || '|' || classification || '|' || stable_fact_key || '|' || COALESCE(encode(prior_state, 'hex'), '')
		   || '|' || COALESCE(encode(current_state, 'hex'), '') || '|' || current_tombstoned
		 FROM changed_since_link_deltas`,
		`SELECT 'changed_since_link_bucket_counts|' || scope_id || '|' || generation_id || '|' || prior_generation_id || '|'
		   || fact_category || '|' || classification || '|' || key_count FROM changed_since_link_bucket_counts`,
		`SELECT 'changed_since_key_state|' || scope_id || '|||' || fact_category || '|' || stable_fact_key || '|' || fact_kind
		   || '|' || encode(state, 'hex') || '|' || COALESCE(owner_uri, '') FROM changed_since_key_state`,
		`SELECT 'changed_since_scope_cursor|' || scope_id || '|||' || COALESCE(state_generation_id, '') || '|'
		   || state_activation_seq FROM changed_since_scope_cursor`,
	} {
		rows, err := q.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("snapshot %q: %v", firstLine(query), err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("snapshot scan: %v", err)
			}
			out = append(out, s)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("snapshot rows: %v", err)
		}
		_ = rows.Close()
	}
	slices.Sort(out)
	return out
}

// namesGeneration reports whether ruling 2.8 retires a snapshot row when gens
// are pruned: a link, link-delta or bucket-count row whose generation or
// prior generation is one of gens, or an activation row of one of gens (an
// activation whose recorded prior was pruned is kept). Generation ids are
// globally unique (scope_generations' primary key), so the scope need not
// match.
func namesGeneration(row string, gens []string) bool {
	fields := strings.SplitN(row, "|", 5)
	if fields[0] == "changed_since_activations" {
		return slices.Contains(gens, fields[2])
	}
	return slices.Contains(gens, fields[2]) || slices.Contains(gens, fields[3])
}

func countByTable(rows []string) map[string]int64 {
	counts := make(map[string]int64)
	for _, row := range rows {
		counts[strings.SplitN(row, "|", 2)[0]]++
	}
	return counts
}

// seedChain seeds a scope of full generations gens (all but the last
// superseded at supersededAt plus i minutes, the last active), gives them
// alternating fact sets and journals them in order. It links nothing.
func (l *ledgerDB) seedChain(t *testing.T, scopeID string, gens []string, supersededAt time.Time) {
	t.Helper()
	times := make([]time.Time, len(gens))
	for i := range times {
		times[i] = supersededAt.Add(time.Duration(i) * time.Minute)
	}
	l.seedChainAt(t, scopeID, gens, times)
}

// seedChainAt is seedChain with one superseded_at per generation (the last
// entry is ignored: that generation is active).
func (l *ledgerDB) seedChainAt(t *testing.T, scopeID string, gens []string, supersededAt []time.Time) {
	t.Helper()
	l.seedScope(t, scopeID)
	for i, gen := range gens {
		activated := fixtureEpoch.Add(time.Duration(i) * time.Hour)
		if i == len(gens)-1 {
			l.seedGeneration(t, scopeID, gen, false, "active", activated, time.Time{})
			l.setActive(t, scopeID, gen)
		} else {
			l.seedGeneration(t, scopeID, gen, false, "superseded", activated, supersededAt[i])
		}
		facts := baseFacts()
		if i%2 == 1 {
			facts = nextFacts()
		}
		l.insertFacts(t, scopeID, gen, facts)
		prior := ""
		if i > 0 {
			prior = gens[i-1]
		}
		l.journal(t, scopeID, gen, prior)
	}
}

// linkChain seeds a chain with seedChain and links every activation.
func (l *ledgerDB) linkChain(t *testing.T, w *linksfreshnessstore.LinkWriter, scopeID string, gens []string, supersededAt time.Time) {
	t.Helper()
	l.seedChain(t, scopeID, gens, supersededAt)
	for range gens {
		mustLink(t, w, l, scopeID)
	}
	if idle := mustLink(t, w, l, scopeID); !idle.Idle {
		t.Fatalf("%s: chain not fully linked: %+v", scopeID, idle)
	}
}

func retentionGenerationHash(generationID string) string {
	sum := sha256.Sum256([]byte("generation\x00" + generationID))
	return hex.EncodeToString(sum[:])
}

// orphanProbe is the probe of arbiter ruling arb-7127-3d: links naming a
// generation with no scope_generations row, activation rows of such a
// generation, and bucket-count groups with no link row.
const orphanProbe = `
SELECT
  (SELECT count(*) FROM changed_since_links AS l
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = l.generation_id)
       OR (l.prior_generation_id <> ''
           AND NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = l.prior_generation_id))),
  (SELECT count(*) FROM changed_since_activations AS a
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = a.generation_id)),
  (SELECT count(*) FROM (SELECT DISTINCT scope_id, generation_id, prior_generation_id
                         FROM changed_since_link_bucket_counts) AS b
    WHERE NOT EXISTS (SELECT 1 FROM changed_since_links AS l WHERE l.scope_id = b.scope_id
                        AND l.generation_id = b.generation_id AND l.prior_generation_id = b.prior_generation_id))`

// probe returns the orphan probe's three figures.
func (l *ledgerDB) probe(t *testing.T) (orphanLinks, orphanActivations, headlessBuckets int64) {
	t.Helper()
	if err := l.raw.QueryRowContext(l.ctx, orphanProbe).Scan(&orphanLinks, &orphanActivations, &headlessBuckets); err != nil {
		t.Fatalf("orphan probe: %v", err)
	}
	return orphanLinks, orphanActivations, headlessBuckets
}

// headlessDeltas counts link-delta rows with no link row. It scans the delta
// table, so it belongs in tests only.
func (l *ledgerDB) headlessDeltas(t *testing.T) int64 {
	t.Helper()
	return l.queryInt(t, `
SELECT count(*) FROM changed_since_link_deltas AS d
WHERE NOT EXISTS (SELECT 1 FROM changed_since_links AS l WHERE l.scope_id = d.scope_id
                    AND l.generation_id = d.generation_id AND l.prior_generation_id = d.prior_generation_id)`)
}
