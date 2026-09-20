// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	querycodedivergence "github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	reducercodedivergence "github.com/eshu-hq/eshu/go/internal/reducer/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PostgresCodeDriftedEvidenceLoader builds the LSH-nominated candidate
// pairs the code_drifted reducer handler verifies. It runs four narrow
// queries per repo, all served by the migration-111 indexes and the entity
// primary key: the band self-join with per-entity budget ranking, the member
// row lookup for kept pairs, and the two exclusion counters. It never reads
// source_cache: verification runs over the persisted shingle sets only.
type PostgresCodeDriftedEvidenceLoader struct {
	DB db.Queryer
	// Tracer wraps LoadCandidates in a single span. Optional; nil disables
	// span emission.
	Tracer trace.Tracer
	// Logger receives WARN logs when a persisted shingle set fails to
	// decode. A decode failure indicates real corruption or a payload
	// schema break; the pair drops and counts as no_shingles rather than
	// failing the generation. Optional; nil drops logs.
	Logger *slog.Logger
}

// LoadCandidates implements reducercodedivergence.CandidateLoader for one
// repo_id. Pairs whose member row is missing (entity churned between the
// pair and member reads) or whose shingle set fails to decode drop with a
// no_shingles count: the loader never emits a pair the handler cannot
// verify.
func (l PostgresCodeDriftedEvidenceLoader) LoadCandidates(
	ctx context.Context,
	repoID string,
) (reducercodedivergence.CandidatePage, error) {
	if l.DB == nil {
		return reducercodedivergence.CandidatePage{}, fmt.Errorf("code drifted evidence database is required")
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return reducercodedivergence.CandidatePage{}, fmt.Errorf("code drifted repo ID must not be blank")
	}

	if l.Tracer != nil {
		var span trace.Span
		ctx, span = l.Tracer.Start(ctx, telemetry.SpanReducerCodeDriftedEvidenceLoad)
		defer span.End()
	}

	pairs, exhausted, err := l.loadPairs(ctx, repoID)
	if err != nil {
		return reducercodedivergence.CandidatePage{}, err
	}
	members, corrupt, err := l.loadMembers(ctx, repoID, pairs)
	if err != nil {
		return reducercodedivergence.CandidatePage{}, err
	}
	page := reducercodedivergence.CandidatePage{Stats: reducercodedivergence.CandidateStats{
		BudgetExhausted: exhausted,
		NoShingles:      len(corrupt),
	}}
	for _, p := range pairs {
		a, okA := members[p.e1]
		b, okB := members[p.e2]
		if !okA || !okB {
			// A member absent from the map is either corrupt (already
			// counted) or churned between the pair and member reads
			// (counted here): either way the pair is unverifiable.
			if !corrupt[p.e1] && !corrupt[p.e2] {
				page.Stats.NoShingles++
			}
			continue
		}
		page.Pairs = append(page.Pairs, reducercodedivergence.CandidatePair{
			A: a, B: b, SharedBands: p.shared,
		})
	}
	if err := l.loadExclusions(ctx, repoID, &page.Stats); err != nil {
		return reducercodedivergence.CandidatePage{}, err
	}
	return page, nil
}

type driftedPairRow struct {
	e1, e2 string
	shared int
	c1, c2 int
}

// loadPairs runs the band self-join nomination and collects the distinct
// budget-exhausted entity IDs: an entity with more partners than the budget
// appears in its kept top-K rows carrying the over-budget total.
func (l PostgresCodeDriftedEvidenceLoader) loadPairs(
	ctx context.Context,
	repoID string,
) ([]driftedPairRow, []string, error) {
	rows, err := l.DB.QueryContext(ctx, listCodeDriftedPairsQuery,
		repoID, querycodedivergence.TokenFloor, reducercodedivergence.MaxCandidatesPerEntity)
	if err != nil {
		return nil, nil, fmt.Errorf("list drifted pairs for repo %q: %w", repoID, err)
	}
	defer func() { _ = rows.Close() }()
	pairs := []driftedPairRow{}
	exhausted := map[string]bool{}
	for rows.Next() {
		var p driftedPairRow
		if err := rows.Scan(&p.e1, &p.e2, &p.shared, &p.c1, &p.c2); err != nil {
			return nil, nil, fmt.Errorf("scan drifted pair for repo %q: %w", repoID, err)
		}
		pairs = append(pairs, p)
		if p.c1 > reducercodedivergence.MaxCandidatesPerEntity {
			exhausted[p.e1] = true
		}
		if p.c2 > reducercodedivergence.MaxCandidatesPerEntity {
			exhausted[p.e2] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	flagged := make([]string, 0, len(exhausted))
	for id := range exhausted {
		flagged = append(flagged, id)
	}
	sort.Strings(flagged)
	return pairs, flagged, nil
}

// loadMembers fetches the member rows for every entity the kept pairs
// reference and decodes their shingle sets. It returns the member map plus
// the set of entity IDs whose shingle set failed to decode: callers drop
// those pairs (already counted) and count only the remaining member-missing
// pairs as no_shingles.
func (l PostgresCodeDriftedEvidenceLoader) loadMembers(
	ctx context.Context,
	repoID string,
	pairs []driftedPairRow,
) (map[string]reducercodedivergence.MemberRow, map[string]bool, error) {
	members := map[string]reducercodedivergence.MemberRow{}
	corrupt := map[string]bool{}
	if len(pairs) == 0 {
		return members, corrupt, nil
	}
	seen := map[string]bool{}
	ids := make([]string, 0, 2*len(pairs))
	for _, p := range pairs {
		for _, id := range []string{p.e1, p.e2} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	rows, err := l.DB.QueryContext(ctx, listCodeDriftedMembersQuery, repoID, pgarray.StringArray(ids))
	if err != nil {
		return nil, nil, fmt.Errorf("list drifted members for repo %q: %w", repoID, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m reducercodedivergence.MemberRow
		var shinglesHex string
		if err := rows.Scan(
			&m.EntityID, &m.FPExact, &m.FPRenamed, &shinglesHex, &m.TokenCount,
			&m.EntityName, &m.EntityType, &m.RelativePath, &m.Language,
			&m.StartLine, &m.EndLine,
		); err != nil {
			return nil, nil, fmt.Errorf("scan drifted member for repo %q: %w", repoID, err)
		}
		shingles, err := fingerprint.DecodeShingles(shinglesHex)
		if err != nil {
			corrupt[m.EntityID] = true
			if l.Logger != nil {
				l.Logger.WarnContext(ctx, "code drifted member shingle decode failed",
					"repo_id", repoID,
					"entity_id", m.EntityID,
					telemetry.LogKeyFailureClass, "shingle_decode",
				)
			}
			continue
		}
		m.Shingles = shingles
		members[m.EntityID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return members, corrupt, nil
}

// loadExclusions fills the row-shaped and pair-shaped pipeline filter
// counts: below-floor and shingle-less rows plus equality-owned pairs.
func (l PostgresCodeDriftedEvidenceLoader) loadExclusions(
	ctx context.Context,
	repoID string,
	stats *reducercodedivergence.CandidateStats,
) error {
	rows, err := l.DB.QueryContext(ctx, countCodeDriftedExclusionsQuery,
		repoID, querycodedivergence.TokenFloor)
	if err != nil {
		return fmt.Errorf("count drifted exclusions for repo %q: %w", repoID, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return fmt.Errorf("count drifted exclusions for repo %q: no row", repoID)
	}
	// Accumulate onto the corrupt-member count the member load already
	// recorded: both name rows no usable shingle set exists for.
	var shingless int
	if err := rows.Scan(&stats.BelowFloor, &shingless); err != nil {
		return fmt.Errorf("scan drifted exclusions for repo %q: %w", repoID, err)
	}
	stats.NoShingles += shingless
	if err := rows.Err(); err != nil {
		return err
	}
	dups, err := l.DB.QueryContext(ctx, countCodeDriftedEqualityDuplicatesQuery,
		repoID, querycodedivergence.TokenFloor)
	if err != nil {
		return fmt.Errorf("count drifted equality duplicates for repo %q: %w", repoID, err)
	}
	defer func() { _ = dups.Close() }()
	if !dups.Next() {
		return fmt.Errorf("count drifted equality duplicates for repo %q: no row", repoID)
	}
	if err := dups.Scan(&stats.EqualityDuplicates); err != nil {
		return fmt.Errorf("scan drifted equality duplicates for repo %q: %w", repoID, err)
	}
	return dups.Err()
}
