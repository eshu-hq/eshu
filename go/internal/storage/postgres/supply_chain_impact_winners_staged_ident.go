// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ResolveCandidateIdents maps staged candidate keys to the identifiers the
// under-lock keyed verification probes. A key present in the stored winners
// resolves from the stored row; a new key resolves from its winning fact's
// payload by point lookup on the reader. A key whose fact is gone entirely
// (deleted mid-pass) resolves to nothing: the writer verification re-derives
// winners from facts, so with no facts it cannot emit a repair, and the skip
// is counted, never silent.
//
// Stored-first resolution is exact, not approximate: every identifier column
// (package, repository, digest, CVE, advisory) is a canonical-key component,
// so for one key the stored and recomputed identifiers are identical by
// construction. The C0 rule matches the candidate scan: CVE id, else advisory
// id, else empty.
func ResolveCandidateIdents(
	ctx context.Context,
	reader db.Queryer,
	keys []string,
	winners map[string]StagedWinner,
	overrides map[string]StagedOverride,
	stored map[string]StoredWinner,
) ([]WinnersAuditIdent, int, error) {
	idents := make([]WinnersAuditIdent, 0, len(keys))
	unresolvable := 0
	for _, key := range keys {
		if current, ok := stored[key]; ok {
			idents = append(idents, WinnersAuditIdent{
				PackageID:     stagedNullString(current.PackageID),
				RepositoryID:  stagedNullString(current.RepositoryID),
				SubjectDigest: stagedNullString(current.SubjectDigest),
				C0:            stagedNullString(firstNonEmpty(current.CVEID, current.AdvisoryID)),
				AdvisoryID:    stagedNullString(current.AdvisoryID),
			})
			continue
		}
		factID := ""
		if winner, ok := winners[key]; ok {
			factID = winner.WinnerFactID
		} else if override, ok := overrides[key]; ok {
			factID = override.FactID
		}
		if factID == "" {
			unresolvable++
			continue
		}
		ident, found, err := fetchWinnerFactIdent(ctx, reader, factID)
		if err != nil {
			return nil, 0, err
		}
		if !found {
			unresolvable++
			continue
		}
		idents = append(idents, ident)
	}
	return idents, unresolvable, nil
}

// stagedWinnerFactIdentSQL point-fetches the identifier columns of one winner
// fact for candidate seeding. It runs on the pinned reader member as one more
// short statement; new keys are rare, so this never approaches batch cost.
const stagedWinnerFactIdentSQL = `SELECT
    COALESCE(NULLIF(fact.payload->>'package_id', ''), ''),
    COALESCE(NULLIF(fact.payload->>'repository_id', ''), ''),
    COALESCE(NULLIF(fact.payload->>'subject_digest', ''), ''),
    COALESCE(NULLIF(fact.payload->>'cve_id', ''), ''),
    COALESCE(NULLIF(fact.payload->>'advisory_id', ''), '')
FROM fact_records AS fact
WHERE fact.fact_id = $1`

func fetchWinnerFactIdent(ctx context.Context, reader db.Queryer, factID string) (WinnersAuditIdent, bool, error) {
	var none WinnersAuditIdent
	if reader == nil {
		return none, false, fmt.Errorf("staged winners ident database is required")
	}
	rows, err := reader.QueryContext(ctx, stagedWinnerFactIdentSQL, factID)
	if err != nil {
		return none, false, fmt.Errorf("read staged winners ident fact: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pkg, repo, digest, cve, advisory string
	found := false
	for rows.Next() {
		if err := rows.Scan(&pkg, &repo, &digest, &cve, &advisory); err != nil {
			return none, false, fmt.Errorf("scan staged winners ident fact: %w", err)
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return none, false, fmt.Errorf("read staged winners ident fact: %w", err)
	}
	if !found {
		return none, false, nil
	}
	return WinnersAuditIdent{
		PackageID:     stagedNullString(pkg),
		RepositoryID:  stagedNullString(repo),
		SubjectDigest: stagedNullString(digest),
		C0:            stagedNullString(firstNonEmpty(cve, advisory)),
		AdvisoryID:    stagedNullString(advisory),
	}, true, nil
}

func stagedNullString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// VerifyStagedCandidates runs the unchanged writer-side verification for one
// identifier set: seed, lock claim, keyed re-verify, queue repairs. It is the
// shared tail of the split scan and the staged standby scan (#10451); the
// read phase that produced the identifiers differs, this phase is identical.
func (s SupplyChainImpactWinnersStore) VerifyStagedCandidates(
	ctx context.Context,
	idents []WinnersAuditIdent,
	read time.Duration,
) (supplychaincore.SupplyChainImpactWinnersAudit, error) {
	var none supplychaincore.SupplyChainImpactWinnersAudit
	beginner, ok := s.DB.(db.Beginner)
	if !ok {
		return none, fmt.Errorf("supply chain impact winners database must support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin supply chain impact winners audit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The seed lands before the lock claim so the single-flight resweep lock
	// is held only for the verification, not for the batched round trips. A
	// lost claim rolls the seeded temp tables back with the transaction.
	if _, err := tx.ExecContext(ctx, createWinnersAuditStaleIdentsSQL); err != nil {
		return none, fmt.Errorf("verify supply chain impact winners audit candidates: %w", err)
	}
	if err := seedWinnersAuditStaleIdents(ctx, tx, idents); err != nil {
		return none, err
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedIdentsSQL); err != nil {
		return none, fmt.Errorf("verify supply chain impact winners audit candidates: %w", err)
	}
	claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx)
	if err != nil {
		return none, err
	}
	if !claimed {
		return supplychaincore.SupplyChainImpactWinnersAudit{Skipped: true, ReadDuration: read}, nil
	}
	for _, stmt := range []string{
		buildWinnersKeyedActiveFactsSQL, addWinnersKeyedOperatorFactsSQL, analyzeWinnersKeyedActiveFactsSQL,
		createWinnersAuditStaleSQL, createWinnersAuditPendingSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return none, fmt.Errorf("verify supply chain impact winners audit candidates: %w", err)
		}
	}
	queued, err := tx.ExecContext(ctx, queueWinnersAuditRepairsSQL)
	if err != nil {
		return none, fmt.Errorf("queue supply chain impact winners audit repairs: %w", err)
	}
	repairs, err := queued.RowsAffected()
	if err != nil {
		return none, fmt.Errorf("read supply chain impact winners audit repairs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return none, fmt.Errorf("commit supply chain impact winners audit: %w", err)
	}
	return supplychaincore.SupplyChainImpactWinnersAudit{Repairs: repairs, ReadDuration: read}, nil
}
