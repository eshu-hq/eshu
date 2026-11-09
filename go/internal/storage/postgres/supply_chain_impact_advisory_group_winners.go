// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// advisoryGroupWinnerHiddenStatesSQL renders the shared hidden-suppression
// list for the winners reader. It is derived from
// supplychaincore.SupplyChainSuppressionHiddenStates (the evaluation.go:83-91
// source: not_affected, accepted_risk, false_positive, ignored,
// advisory_retracted) so a new hidden state hides here without a second edit;
// expired, provider_dismissed, and scope_mismatch stay visible by construction.
func advisoryGroupWinnerHiddenStatesSQL() string {
	hidden := supplychaincore.SupplyChainSuppressionHiddenStates()
	quoted := make([]string, 0, len(hidden))
	for _, state := range hidden {
		quoted = append(quoted, "'"+string(state)+"'")
	}
	return strings.Join(quoted, ",")
}

// advisoryGroupWinnerQualifyingStatusesSQL renders the qualifying impact set
// for the winners reader, derived from the impact-status vocabulary
// (impact_status.go): exact, derived, and possibly-affected findings project;
// known-fixed and unknown findings never do.
func advisoryGroupWinnerQualifyingStatusesSQL() string {
	qualifying := []supplychaincore.SupplyChainImpactStatus{
		supplychaincore.SupplyChainImpactAffectedExact,
		supplychaincore.SupplyChainImpactAffectedDerived,
		supplychaincore.SupplyChainImpactPossiblyAffected,
	}
	quoted := make([]string, 0, len(qualifying))
	for _, status := range qualifying {
		quoted = append(quoted, "'"+string(status)+"'")
	}
	return strings.Join(quoted, ",")
}

// advisoryGroupWinnerEffectiveStateSQL renders the expiry-aware effective
// suppression state over one winners row. It replicates the predicate shape of
// findings_queries.go:68-83 (supplyChainImpactEffectiveDecisionStateSQL) with
// the winners column type: suppression_expires_at is already a TIMESTAMPTZ, so
// the NULL branch covers "no expiry" and the text-validity pg_input_is_valid
// branch of the findings read has no typed-column equivalent; an expired
// suppression reads 'expired' and stays visible via the hidden-set filter.
func advisoryGroupWinnerEffectiveStateSQL() string {
	hidden := advisoryGroupWinnerHiddenStatesSQL()
	return `CASE
      WHEN w.suppression_state NOT IN (` + hidden + `)
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END`
}

// advisoryGroupWinnersQuery selects the qualifying canonical winners behind
// one advisory group. The group matches on winners columns only (CVE when the
// row names one, else the advisory id), so the read never scans facts by CVE.
// Members and purl come only from the winner fact payload via winner_fact_id
// (fact_records.fact_id, the same join key the winners list read uses), and
// the protected-scan exclusion is the always-on subset of the static-winner
// predicate (finding_authority_sql.go:62-72): no scan association and no
// derived image scope. The classification EXISTS arm of that predicate is TRUE
// under non-protected reads, so it carries no rows here.
var advisoryGroupWinnersQuery = `SELECT w.repository_id,
       w.package_id,
       w.ecosystem,
       COALESCE(f.payload->>'purl', ''),
       w.cve_id,
       w.advisory_id,
       f.payload->'advisory_group_members'
FROM supply_chain_impact_canonical_winners AS w
LEFT JOIN fact_records AS f
  ON f.fact_id = w.winner_fact_id
WHERE (CASE WHEN btrim(w.cve_id) <> '' THEN w.cve_id ELSE w.advisory_id END) = $1
  AND w.impact_status IN (` + advisoryGroupWinnerQualifyingStatusesSQL() + `)
  AND ` + advisoryGroupWinnerEffectiveStateSQL() + ` NOT IN (` + advisoryGroupWinnerHiddenStatesSQL() + `)
  AND w.repository_id <> ''
  AND w.winner_scope_id NOT LIKE 'image://derived/%'
  AND NOT EXISTS (
        SELECT 1 FROM scanner_image_result_fact_associations AS finding_association
        WHERE finding_association.finding_fact_id = w.winner_fact_id
      )
ORDER BY w.repository_id, w.package_id`

// QualifyingAdvisoryGroupWinners returns the qualifying canonical winners
// behind one advisory group as advisorygroup.WinnerRow values, in
// repository/package order, with members and purl resolved from each winner's
// fact. A winner without a fact row still projects with an empty purl and nil
// members.
func (s FactStore) QualifyingAdvisoryGroupWinners(ctx context.Context, groupID string) ([]advisorygroup.WinnerRow, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	rows, err := s.database.QueryContext(ctx, advisoryGroupWinnersQuery, groupID)
	if err != nil {
		return nil, fmt.Errorf("qualifying advisory group winners: %w", err)
	}
	defer func() { _ = rows.Close() }()

	winners := make([]advisorygroup.WinnerRow, 0, 64)
	for rows.Next() {
		var row advisorygroup.WinnerRow
		var membersJSON []byte
		if err := rows.Scan(
			&row.RepositoryID,
			&row.PackageID,
			&row.Ecosystem,
			&row.PURL,
			&row.CVEID,
			&row.AdvisoryID,
			&membersJSON,
		); err != nil {
			return nil, fmt.Errorf("qualifying advisory group winners scan: %w", err)
		}
		if len(membersJSON) > 0 {
			var members []string
			if err := json.Unmarshal(membersJSON, &members); err != nil {
				return nil, fmt.Errorf("qualifying advisory group winners members: %w", err)
			}
			row.Members = members
		}
		winners = append(winners, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("qualifying advisory group winners: %w", err)
	}
	return winners, nil
}

// AdvisoryGroupWinnersWatermark returns the winners maintainer's
// last-successful-rebuild timestamp from its singleton watermark row
// (supply_chain_impact_winners_materialization). The singleton is the
// freshness signal: the rebuild stamps it on every resweep even when no row
// changed, while a winner's materialized_at means "last content change" and
// would misreport freshness after a no-change resweep. Zero time with no error
// means the maintainer never populated the table.
func (s FactStore) AdvisoryGroupWinnersWatermark(ctx context.Context) (time.Time, error) {
	if s.database == nil {
		return time.Time{}, fmt.Errorf("fact store database is required")
	}
	rows, err := s.database.QueryContext(ctx, selectSupplyChainImpactWinnersWatermarkSQL)
	if err != nil {
		return time.Time{}, fmt.Errorf("advisory group winners watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, fmt.Errorf("advisory group winners watermark: %w", err)
		}
		return time.Time{}, nil
	}
	var at time.Time
	if err := rows.Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("advisory group winners watermark scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, fmt.Errorf("advisory group winners watermark: %w", err)
	}
	return at, nil
}
