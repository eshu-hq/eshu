// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package digest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Cassettes are the committed cassettes the impact family drives, in commit order, relative to
// testdata/cassettes: the vulnerability intelligence scopes, the SBOM documents and the package
// registry whose purls and package ids the findings join on.
var Cassettes = []string{
	"vulnerabilityintelligence/supply-chain-demo.json",
	"sbomattestation/supply-chain-demo.json",
	"packageregistry/supply-chain-demo.json",
}

// DigestSQL is the family digest: sha256 over every support row (the payload without intent_id, cause and
// the observation timestamp provenance.first_seen_at) ordered by fact id, a separator, and every winner row
// (without the materialization stamp and the same first_seen_at) ordered by
// canonical key. It covers the stable key, the content digest
// and the evidence ids (they are payload) and every derived column.
const DigestSQL = `
SELECT encode(sha256(convert_to(
    (SELECT COALESCE(string_agg(
        (to_jsonb(s) - 'payload' || jsonb_build_object('payload', (s.payload - 'intent_id' - 'cause') #- '{provenance,first_seen_at}'))::text,
        E'\n' ORDER BY s.fact_id), '')
     FROM supply_chain_impact_finding_supports AS s)
    || E'\n--winners--\n' ||
    (SELECT COALESCE(string_agg((to_jsonb(w) - 'materialized_at' || jsonb_build_object('payload', w.payload #- '{provenance,first_seen_at}'))::text, E'\n' ORDER BY w.canonical_key), '')
     FROM supply_chain_impact_canonical_winners AS w),
    'UTF8')), 'hex')`

// countsSQL counts the rows the digest covers, so an empty family cannot hash equal to another empty one.
const countsSQL = `
SELECT (SELECT count(*) FROM supply_chain_impact_finding_supports),
       (SELECT count(*) FROM supply_chain_impact_canonical_winners)`

// Queryer is the read surface Read needs; *sql.DB and *sql.Tx satisfy it.
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Result is one digest read.
type Result struct {
	// Digest is the hex sha256 of the supports and winners.
	Digest string
	// Supports and Winners are the row counts the digest covers.
	Supports, Winners int
}

// Read returns the digest and the row counts it covers. It fails when either table is empty: a family that
// derived nothing must not pass by comparing two empty digests.
func Read(ctx context.Context, q Queryer) (Result, error) {
	var r Result
	if err := q.QueryRowContext(ctx, countsSQL).Scan(&r.Supports, &r.Winners); err != nil {
		return Result{}, fmt.Errorf("count supports and winners: %w", err)
	}
	if r.Supports == 0 || r.Winners == 0 {
		return Result{}, fmt.Errorf("the impact family derived %d supports and %d winners: the cassette composition yields no findings, so the digest would compare empty tables", r.Supports, r.Winners)
	}
	if err := q.QueryRowContext(ctx, DigestSQL).Scan(&r.Digest); err != nil {
		return Result{}, fmt.Errorf("digest supports and winners: %w", err)
	}
	return r, nil
}

// Pinned returns the first line of a pin file (testdata/golden/ifa-impact-family-digest.txt) that is neither
// blank nor a '#' comment, or "" when the file pins nothing yet.
func Pinned(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}
