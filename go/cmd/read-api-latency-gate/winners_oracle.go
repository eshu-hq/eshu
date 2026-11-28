// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
)

// OracleCounts is the independently computed truth a winners-leg findings
// page must serve: row and canonical-group counts read straight from
// supply_chain_impact_finding_supports. Since the #10880 PR-C2 write-stop
// (migration 358 refuses finding-fact writes to fact_records) the seeded
// corpus lives only in the supports table, so the oracle reads there —
// never through the read API it validates.
type OracleCounts struct {
	SupportsRows    int64
	CanonicalGroups int64
}

// QuerySupportsOracle reads the oracle counts from the seeded Postgres. The
// query is one indexed aggregation over the supports table, not the read
// API's dedup path, so it stays independent of the code under proof.
func QuerySupportsOracle(ctx context.Context, db *sql.DB) (OracleCounts, error) {
	var counts OracleCounts
	if err := db.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT canonical_key) FROM supply_chain_impact_finding_supports`).Scan(
		&counts.SupportsRows, &counts.CanonicalGroups); err != nil {
		return OracleCounts{}, fmt.Errorf("count supply chain impact finding supports oracle: %w", err)
	}
	return counts, nil
}

// CheckSupportsOracle fails unless the supports table holds exactly the
// seeded corpus: every seeded support row and one canonical group per seeded
// group. A bare nonzero count is not enough — a partial seed would leave the
// winners leg measuring a smaller read model than the budgets assume.
func CheckSupportsOracle(counts OracleCounts, total, sourcesPerGroup int) error {
	if counts.SupportsRows != int64(total) {
		return fmt.Errorf("supply_chain_impact_finding_supports holds %d rows, want %d seeded supports (a partial seed leaves the winners leg measuring a smaller read model)", counts.SupportsRows, total)
	}
	wantGroups := expectedWinnerGroups(total, sourcesPerGroup)
	if counts.CanonicalGroups != wantGroups {
		return fmt.Errorf("supply_chain_impact_finding_supports holds %d canonical groups, want %d (one per seeded group)", counts.CanonicalGroups, wantGroups)
	}
	return nil
}

// winnersFindingsPage is the subset of the findings-list envelope the oracle
// proof reads: the relayed findings array plus the server's own count, limit,
// and truncation flags.
type winnersFindingsPage struct {
	Findings  []json.RawMessage `json:"findings"`
	Count     int               `json:"count"`
	Limit     int               `json:"limit"`
	Truncated bool              `json:"truncated"`
}

// VerifyWinnersFindingsPage fails unless the winners-leg snapshot serves the
// oracle truth: the page is non-empty, the envelope count matches the array
// it carries, and the truncated flag agrees with the oracle about whether
// more truth sits beyond the page. A page that is empty, self-inconsistent,
// or truncated differently than the oracle depth implies proves nothing about
// the winners read path.
func VerifyWinnersFindingsPage(oracleGroups int64, page []byte) error {
	var parsed winnersFindingsPage
	if err := json.Unmarshal(page, &parsed); err != nil {
		return fmt.Errorf("parse winners findings page: %w", err)
	}
	if parsed.Findings == nil {
		return fmt.Errorf("winners findings page has no findings field: nothing proves the leg served findings")
	}
	if len(parsed.Findings) == 0 {
		return fmt.Errorf("winners findings page holds no findings: a vacuous winners leg proves nothing")
	}
	if parsed.Count != len(parsed.Findings) {
		return fmt.Errorf("winners findings page count %d disagrees with its %d findings: the envelope is not self-consistent", parsed.Count, len(parsed.Findings))
	}
	if parsed.Limit <= 0 {
		return fmt.Errorf("winners findings page limit %d is not positive: truncation cannot be checked", parsed.Limit)
	}
	if wantTruncated := oracleGroups > int64(parsed.Limit); parsed.Truncated != wantTruncated {
		return fmt.Errorf("winners findings page truncated=%v disagrees with the %d-group oracle at limit %d: the leg did not serve the full seeded truth", parsed.Truncated, oracleGroups, parsed.Limit)
	}
	return nil
}

// verifyWinnersOracle is the -verify-winners-oracle entry: it queries the
// supports oracle from the seeded Postgres, checks it against the seeded
// corpus size, and proves the relayed winners snapshot serves that truth. It
// prints the measured/vacuous verdict per leg for the gate log (#11644: the
// gate output names the post-C2 scope each leg actually covers).
func verifyWinnersOracle(ctx context.Context, opts runOptions) error {
	if opts.oraclePagePath == "" {
		return fmt.Errorf("-oracle-page is required with -verify-winners-oracle (path to the winners-leg findings snapshot)")
	}
	if opts.winnersFactCount <= 0 {
		return fmt.Errorf("-winners-fact-count must be positive with -verify-winners-oracle (the oracle checks the seeded corpus size)")
	}
	db, err := sql.Open("pgx", opts.postgresDSN)
	if err != nil {
		return fmt.Errorf("open postgres for winners oracle: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres for winners oracle: %w", err)
	}
	counts, err := QuerySupportsOracle(ctx, db)
	if err != nil {
		return err
	}
	if err := CheckSupportsOracle(counts, opts.winnersFactCount, winnersSourcesPerGroup); err != nil {
		return err
	}
	page, err := os.ReadFile(opts.oraclePagePath)
	if err != nil {
		return fmt.Errorf("read winners findings snapshot %s: %w", opts.oraclePagePath, err)
	}
	if err := VerifyWinnersFindingsPage(counts.CanonicalGroups, page); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: winners findings oracle measured (%d canonical groups over %d supports served on the relayed page)\n",
		counts.CanonicalGroups, counts.SupportsRows)
	return nil
}
