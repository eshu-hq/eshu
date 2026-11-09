// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Lane B of the staged stale-winners audit scan (eshu-hq/eshu#10451): the
// streaming compaction of per-pair batches into per-key winners plus the
// superset diff against the stored winners table.
//
// A standby that cannot run the single-statement audit fetches one narrow
// batch per active (scope, generation) pair (lane A) and folds every batch
// into the in-memory maps here, one batch at a time, without ever holding
// all staged rows. The coordinator then diffs the compacted winners against
// the stored table and hands only the differing keys to the writer-side
// verification, which stays authoritative.
//
// Fail-closed caps arrive as constructor parameters (lane C wires its
// constants at integration). An over-budget batch aborts with a typed error
// and is dropped whole: the compactor never truncates.

// StagedCandidateRow is one narrow candidate row fed to the compactor. Its
// shape is identical to lane A's StagedRow; lane A owns that type on its
// branch, so this branch carries its own struct and the integration unifies
// them with a type alias. The coordinator routes operator-suppressions rows
// to AddOverrideBatch and every other scope's rows to AddSourceBatch.
type StagedCandidateRow struct {
	FactID              string
	CanonicalKey        string
	FindingID           string
	PriorityScore       int
	HasPayloadFindingID bool
	SuppressionState    string
}

// StagedWinner is the compacted source winner of one canonical key: the
// winning row's fields plus the additive source count across batches.
type StagedWinner struct {
	CanonicalKey        string
	WinnerFactID        string
	FindingID           string
	PriorityScore       int
	HasPayloadFindingID bool
	SuppressionState    string
	SourceCount         int
}

// StagedOverride is the compacted operator override of one canonical key.
// Overrides never touch source winners; they merge only in DiffCandidates.
type StagedOverride struct {
	CanonicalKey        string
	FactID              string
	PriorityScore       int
	HasPayloadFindingID bool
	SuppressionState    string
}

// StoredWinner carries every content column the FULL JOIN ROW(...) IS
// DISTINCT FROM ROW(...) comparison reads: the canonical key plus every
// entry of supplyChainImpactWinnerDataColumns in table order.
type StoredWinner struct {
	CanonicalKey         string
	WinnerFactID         string
	WinnerScopeID        string
	FindingID            string
	PriorityScore        int
	SourceCount          int
	ImpactStatus         string
	Ecosystem            string
	SeverityBucket       string
	RepositoryID         string
	CVEID                string
	AdvisoryID           string
	PackageID            string
	SubjectDigest        string
	ImageRef             string
	PriorityBucket       string
	DetectionProfile     string
	ObservedVersion      string
	MatchReason          string
	SuppressionState     string
	SuppressionExpiresAt sql.NullTime
	ServiceIDs           string
	WorkloadIDs          string
	Environments         string
}

// StagedCompactionOverBudgetError marks a batch or tick that tripped a
// fail-closed cap. What names the tripped bound ("batch rows", "staged
// facts", or "distinct keys").
type StagedCompactionOverBudgetError struct {
	What  string
	Got   int
	Limit int
}

func (e *StagedCompactionOverBudgetError) Error() string {
	return fmt.Sprintf("staged compaction over budget: %s got %d, limit %d", e.What, e.Got, e.Limit)
}

// asStagedOverBudget reports whether err is a compaction over-budget abort.
func asStagedOverBudget(err error) (*StagedCompactionOverBudgetError, bool) {
	var over *StagedCompactionOverBudgetError
	if err == nil || !errors.As(err, &over) {
		return nil, false
	}
	return over, true
}

// stagedBetterSource reports whether candidate row beats the incumbent winner
// under the shipped total order: priority_score DESC, then the payload
// finding-id flag DESC, then fact_id ASC. fact_id is unique, so the order is
// total and batch order cannot change the outcome.
func stagedBetterSource(candidate StagedCandidateRow, incumbent StagedWinner) bool {
	if candidate.PriorityScore != incumbent.PriorityScore {
		return candidate.PriorityScore > incumbent.PriorityScore
	}
	if candidate.HasPayloadFindingID != incumbent.HasPayloadFindingID {
		return candidate.HasPayloadFindingID && !incumbent.HasPayloadFindingID
	}
	return candidate.FactID < incumbent.WinnerFactID
}

// stagedBetterOverride reports whether candidate override beats the
// incumbent under the same total order the shipped operator_overrides CTE
// uses over the same three columns.
func stagedBetterOverride(candidate StagedCandidateRow, incumbent StagedOverride) bool {
	if candidate.PriorityScore != incumbent.PriorityScore {
		return candidate.PriorityScore > incumbent.PriorityScore
	}
	if candidate.HasPayloadFindingID != incumbent.HasPayloadFindingID {
		return candidate.HasPayloadFindingID && !incumbent.HasPayloadFindingID
	}
	return candidate.FactID < incumbent.FactID
}

// StagedCompactor folds streamed batches into per-key winners. It is not safe
// for concurrent use; the tick runner feeds batches from one goroutine.
type StagedCompactor struct {
	maxRowsPerBatch int
	maxStagedFacts  int
	maxDistinctKeys int
	stagedFacts     int
	source          map[string]StagedWinner
	overrides       map[string]StagedOverride
}

// NewStagedCompactor builds a compactor with fail-closed caps. A batch
// holding more than maxRowsPerBatch rows, a tick staging more than
// maxStagedFacts facts, or more than maxDistinctKeys distinct keys aborts
// with *StagedCompactionOverBudgetError; the offending batch is dropped
// whole and earlier state is untouched.
func NewStagedCompactor(maxRowsPerBatch, maxStagedFacts, maxDistinctKeys int) *StagedCompactor {
	return &StagedCompactor{
		maxRowsPerBatch: maxRowsPerBatch,
		maxStagedFacts:  maxStagedFacts,
		maxDistinctKeys: maxDistinctKeys,
		source:          make(map[string]StagedWinner),
		overrides:       make(map[string]StagedOverride),
	}
}

// AddSourceBatch folds one source batch into the per-key source winners.
// Rows with an empty canonical key are skipped: they carry no key to compact.
func (c *StagedCompactor) AddSourceBatch(rows []StagedCandidateRow) error {
	if len(rows) > c.maxRowsPerBatch {
		return &StagedCompactionOverBudgetError{What: "batch rows", Got: len(rows), Limit: c.maxRowsPerBatch}
	}
	if c.stagedFacts+len(rows) > c.maxStagedFacts {
		return &StagedCompactionOverBudgetError{What: "staged facts", Got: c.stagedFacts + len(rows), Limit: c.maxStagedFacts}
	}
	staged := make(map[string]StagedWinner, len(rows))
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.CanonicalKey == "" {
			continue
		}
		if incumbent, ok := staged[row.CanonicalKey]; !ok {
			staged[row.CanonicalKey] = StagedWinner{
				CanonicalKey:        row.CanonicalKey,
				WinnerFactID:        row.FactID,
				FindingID:           row.FindingID,
				PriorityScore:       row.PriorityScore,
				HasPayloadFindingID: row.HasPayloadFindingID,
				SuppressionState:    row.SuppressionState,
			}
		} else if stagedBetterSource(row, incumbent) {
			incumbent.WinnerFactID = row.FactID
			incumbent.FindingID = row.FindingID
			incumbent.PriorityScore = row.PriorityScore
			incumbent.HasPayloadFindingID = row.HasPayloadFindingID
			incumbent.SuppressionState = row.SuppressionState
			staged[row.CanonicalKey] = incumbent
		}
		counts[row.CanonicalKey]++
	}
	if c.DistinctKeys()+newStagedKeys(c.source, c.overrides, staged) > c.maxDistinctKeys {
		return &StagedCompactionOverBudgetError{What: "distinct keys", Got: c.DistinctKeys() + newStagedKeys(c.source, c.overrides, staged), Limit: c.maxDistinctKeys}
	}
	for key, batchWinner := range staged {
		if incumbent, ok := c.source[key]; ok {
			batchWinner.SourceCount = incumbent.SourceCount + counts[key]
			if stagedBetterSource(StagedCandidateRow{
				FactID:              batchWinner.WinnerFactID,
				PriorityScore:       batchWinner.PriorityScore,
				HasPayloadFindingID: batchWinner.HasPayloadFindingID,
			}, incumbent) {
				batchWinner.SourceCount = incumbent.SourceCount + counts[key]
				c.source[key] = batchWinner
			} else {
				incumbent.SourceCount += counts[key]
				c.source[key] = incumbent
			}
			continue
		}
		batchWinner.SourceCount = counts[key]
		c.source[key] = batchWinner
	}
	c.stagedFacts += len(rows)
	return nil
}

// AddOverrideBatch folds one operator-suppressions batch into the separate
// override map. Source winners are untouched; the merge happens only in
// DiffCandidates after every source batch resolves.
func (c *StagedCompactor) AddOverrideBatch(rows []StagedCandidateRow) error {
	if len(rows) > c.maxRowsPerBatch {
		return &StagedCompactionOverBudgetError{What: "batch rows", Got: len(rows), Limit: c.maxRowsPerBatch}
	}
	if c.stagedFacts+len(rows) > c.maxStagedFacts {
		return &StagedCompactionOverBudgetError{What: "staged facts", Got: c.stagedFacts + len(rows), Limit: c.maxStagedFacts}
	}
	staged := make(map[string]StagedOverride, len(rows))
	for _, row := range rows {
		if row.CanonicalKey == "" {
			continue
		}
		if incumbent, ok := staged[row.CanonicalKey]; !ok {
			staged[row.CanonicalKey] = StagedOverride{
				CanonicalKey:        row.CanonicalKey,
				FactID:              row.FactID,
				PriorityScore:       row.PriorityScore,
				HasPayloadFindingID: row.HasPayloadFindingID,
				SuppressionState:    row.SuppressionState,
			}
		} else if stagedBetterOverride(row, incumbent) {
			incumbent.FactID = row.FactID
			incumbent.PriorityScore = row.PriorityScore
			incumbent.HasPayloadFindingID = row.HasPayloadFindingID
			incumbent.SuppressionState = row.SuppressionState
			staged[row.CanonicalKey] = incumbent
		}
	}
	merged := make(map[string]StagedWinner, len(staged))
	for key, batchOverride := range staged {
		merged[key] = StagedWinner{CanonicalKey: key, WinnerFactID: batchOverride.FactID}
	}
	if c.DistinctKeys()+newStagedKeys(c.source, c.overrides, merged) > c.maxDistinctKeys {
		return &StagedCompactionOverBudgetError{What: "distinct keys", Got: c.DistinctKeys() + newStagedKeys(c.source, c.overrides, merged), Limit: c.maxDistinctKeys}
	}
	for key, batchOverride := range staged {
		if incumbent, ok := c.overrides[key]; ok {
			if stagedBetterOverride(StagedCandidateRow{
				FactID:              batchOverride.FactID,
				PriorityScore:       batchOverride.PriorityScore,
				HasPayloadFindingID: batchOverride.HasPayloadFindingID,
			}, incumbent) {
				c.overrides[key] = batchOverride
			}
			continue
		}
		c.overrides[key] = batchOverride
	}
	c.stagedFacts += len(rows)
	return nil
}

// newStagedKeys counts staged keys absent from both compacted maps.
func newStagedKeys(source map[string]StagedWinner, overrides map[string]StagedOverride, staged map[string]StagedWinner) int {
	n := 0
	for key := range staged {
		if _, ok := source[key]; ok {
			continue
		}
		if _, ok := overrides[key]; ok {
			continue
		}
		n++
	}
	return n
}

// SourceWinners returns the compacted per-key source winners.
func (c *StagedCompactor) SourceWinners() map[string]StagedWinner {
	return c.source
}

// Overrides returns the compacted per-key operator overrides.
func (c *StagedCompactor) Overrides() map[string]StagedOverride {
	return c.overrides
}

// StagedFacts returns the total facts folded in so far.
func (c *StagedCompactor) StagedFacts() int {
	return c.stagedFacts
}

// DistinctKeys returns the distinct keys across both maps.
func (c *StagedCompactor) DistinctKeys() int {
	union := len(c.source)
	for key := range c.overrides {
		if _, ok := c.source[key]; !ok {
			union++
		}
	}
	return union
}

// stagedStoredWinnersSQL fetches every column the FULL JOIN ROW(...)
// comparison reads: the canonical key plus every content column of
// supply_chain_impact_canonical_winners in table order.
const stagedStoredWinnersSQL = `SELECT canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score, source_count, impact_status, ecosystem, severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket, detection_profile, observed_version, match_reason, suppression_state, suppression_expires_at, service_ids, workload_ids, environments FROM supply_chain_impact_canonical_winners ORDER BY canonical_key ASC`

// FetchStoredWinners reads the whole stored winners table into a per-key map.
// The table is the bounded read-model size the distinct-keys cap already
// guards; callers run it after every batch compacts.
func FetchStoredWinners(ctx context.Context, reader db.Queryer) (map[string]StoredWinner, error) {
	if reader == nil {
		return nil, fmt.Errorf("stored winners database is required")
	}
	rows, err := reader.QueryContext(ctx, stagedStoredWinnersSQL)
	if err != nil {
		return nil, fmt.Errorf("fetch stored winners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stored := make(map[string]StoredWinner)
	for rows.Next() {
		var winner StoredWinner
		var serviceIDs, workloadIDs, environments []byte
		if err := rows.Scan(
			&winner.CanonicalKey, &winner.WinnerFactID, &winner.WinnerScopeID,
			&winner.FindingID, &winner.PriorityScore, &winner.SourceCount,
			&winner.ImpactStatus, &winner.Ecosystem, &winner.SeverityBucket,
			&winner.RepositoryID, &winner.CVEID, &winner.AdvisoryID,
			&winner.PackageID, &winner.SubjectDigest, &winner.ImageRef,
			&winner.PriorityBucket, &winner.DetectionProfile,
			&winner.ObservedVersion, &winner.MatchReason,
			&winner.SuppressionState, &winner.SuppressionExpiresAt,
			&serviceIDs, &workloadIDs, &environments,
		); err != nil {
			return nil, fmt.Errorf("scan stored winner: %w", err)
		}
		winner.ServiceIDs = string(serviceIDs)
		winner.WorkloadIDs = string(workloadIDs)
		winner.Environments = string(environments)
		stored[winner.CanonicalKey] = winner
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch stored winners: %w", err)
	}
	return stored, nil
}

// DiffCandidates returns the canonical keys the writer must verify, in
// ascending order. The result is a SUPERSET of what the SQL FULL JOIN diff
// would emit: false positives are discarded by the writer-side verification,
// false negatives are forbidden.
//
//   - A key with an operator override present is always emitted. The override
//     decides suppression_state and suppression_expires_at through the
//     pg_input_is_valid guarded CASE, which Go must not reimplement; forcing
//     emission keeps every override-driven change (including expiry-only
//     changes) in the verified set.
//   - Keys missing on either side (new winners, deleted winners) are always
//     emitted, matching the FULL JOIN's NULL branches.
//   - For a key on both sides with no override, Go compares exactly the
//     narrow fields the staged scan carries (winner_fact_id, finding_id,
//     priority_score, source_count, and the source suppression_state).
//     Winner-payload wide fields (severity_bucket, impact_status, the
//     identifier and profile columns, and the jsonb columns) are pinned by
//     winner_fact_id: the same winning fact carries the same payload the
//     full recompute would project, so any wide-field change arrives with a
//     new winner_fact_id and is emitted. Go never reimplements the severity
//     CASE for the same reason.
func DiffCandidates(sourceWinners map[string]StagedWinner, overrides map[string]StagedOverride, stored map[string]StoredWinner) []string {
	var candidates []string
	for key, winner := range sourceWinners {
		if _, ok := overrides[key]; ok {
			candidates = append(candidates, key)
			continue
		}
		current, ok := stored[key]
		if !ok {
			candidates = append(candidates, key)
			continue
		}
		if stagedWinnerDiffers(winner, current) {
			candidates = append(candidates, key)
		}
	}
	for key := range stored {
		if _, ok := sourceWinners[key]; ok {
			continue
		}
		// An override-only key has no source winner and fabricates no
		// finding, but it is still emitted: a stale stored row under an
		// operator key needs the writer's verdict, and emission here is a
		// false positive at worst.
		candidates = append(candidates, key)
	}
	for key := range overrides {
		if _, ok := sourceWinners[key]; ok {
			continue
		}
		if _, ok := stored[key]; ok {
			continue
		}
		candidates = append(candidates, key)
	}
	sort.Strings(candidates)
	return candidates
}

// stagedWinnerDiffers compares the narrow fields Go carries. Wide payload
// fields are pinned by winner_fact_id per DiffCandidates.
func stagedWinnerDiffers(winner StagedWinner, current StoredWinner) bool {
	if winner.WinnerFactID != current.WinnerFactID {
		return true
	}
	if winner.FindingID != current.FindingID {
		return true
	}
	if winner.PriorityScore != current.PriorityScore {
		return true
	}
	if winner.SourceCount != current.SourceCount {
		return true
	}
	return winner.SuppressionState != current.SuppressionState
}
