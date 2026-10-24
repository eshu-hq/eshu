// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// suppressionScopeTrimCharactersSQL is the Unicode White_Space set the
// suppression scope arms trim with (factimpact owns the spelling).
const suppressionScopeTrimCharactersSQL = factimpact.SuppressionScopeTrimCharactersSQL

// supplyChainImpactPayloadPageSize is how many resolved fact IDs one payload
// page fetches by primary key.
const supplyChainImpactPayloadPageSize = 500

// maxSupplyChainImpactActiveEvidenceRowsPerCall bounds the vulnerability.
// suppression rows one ListActiveSupplyChainImpactFacts call may return
// before it reports truncation (#5466 round-7 review P1-B, round-8 F-3).
//
// It counts ONLY vulnerability.suppression rows, never core evidence: the
// suppression branch is the one statement that returns that kind, and it
// orders by byte order and stops at the cap plus one, while every core branch
// returns every match. Core evidence for a finding can never be crowded out
// by suppression noise sharing the same filter values. A load that finds more
// than the cap keeps the first cap rows in byte order and reports truncation;
// an exact-cap result is complete.
//
// A truncated load fails OPEN for the entire suppression candidate set: the
// returned bool reaches SupplyChainImpactHandler, which discards the retained
// prefix because it cannot prove that prefix contains the globally preferred
// AuthoredAt/ID winner. A `var`, not a `const`, so tests can lower it.
var maxSupplyChainImpactActiveEvidenceRowsPerCall = 2000

// lowerCleanedStringFilterValues lowercases every value and re-runs
// cleanStringFilterValues (trim, drop-empty, dedupe, sort) so the result is a
// stable bind-ready set for normalized suppression identity predicates.
func lowerCleanedStringFilterValues(values []string) []string {
	lowered := make([]string, 0, len(values))
	for _, value := range values {
		lowered = append(lowered, strings.ToLower(value))
	}
	return cleanStringFilterValues(lowered)
}

// ListActiveSupplyChainImpactFacts loads active package, SBOM, image, and
// risk evidence for one bounded supply-chain impact reducer intent. The bool
// reports whether the suppression tail was truncated.
func (s FactStore) ListActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter supplychaincore.SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	envelopes, stats, err := s.ListActiveSupplyChainImpactFactsWithStats(ctx, filter)
	return envelopes, stats.SuppressionTruncated, err
}

// ListActiveSupplyChainImpactFactsWithStats is ListActiveSupplyChainImpactFacts
// plus the per-branch statement accounting the reducer logs (#8880).
//
// One call resolves matching fact IDs once from per-branch, index-anchored
// statements (factimpact.Branches), dedupes and orders them in Go (core by
// byte order, then suppression by byte order under the cap), and fetches the
// payloads in 500-ID primary-key pages. The canonical identity stream keeps
// its own cursor. Everything runs in one read-only repeatable-read snapshot
// on the primary, so the IDs and the pages see one state.
func (s FactStore) ListActiveSupplyChainImpactFactsWithStats(
	ctx context.Context,
	filter supplychaincore.SupplyChainImpactFactFilter,
) ([]facts.Envelope, supplychaincore.SupplyChainImpactActiveEvidenceLoadStats, error) {
	var stats supplychaincore.SupplyChainImpactActiveEvidenceLoadStats
	if s.database == nil {
		return nil, stats, fmt.Errorf("fact store database is required")
	}
	normalizeSupplyChainImpactFactFilter(&filter)
	if supplyChainImpactFactFilterEmpty(filter) {
		return nil, stats, nil
	}
	var loaded []facts.Envelope
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		resolved, err := s.resolveSupplyChainImpactFactIDs(ctx, queryer, filter, &stats)
		if err != nil {
			return err
		}
		core, err := s.loadSupplyChainImpactPayloadPages(ctx, queryer, resolved.core, &stats)
		if err != nil {
			return err
		}
		identity, err := s.loadSupplyChainImpactIdentityStream(ctx, queryer, filter, &stats)
		if err != nil {
			return err
		}
		suppressions, err := s.loadSupplyChainImpactPayloadPages(ctx, queryer, resolved.suppressions, &stats)
		if err != nil {
			return err
		}
		loaded = make([]facts.Envelope, 0, len(core)+len(identity)+len(suppressions))
		loaded = append(append(append(loaded, core...), identity...), suppressions...)
		return nil
	})
	if err != nil {
		return nil, supplychaincore.SupplyChainImpactActiveEvidenceLoadStats{}, err
	}
	s.recordSupplyChainImpactLoad(ctx, filter, stats)
	return loaded, stats, nil
}

// supplyChainImpactResolvedIDs is the ordered ID set one call loads.
type supplyChainImpactResolvedIDs struct {
	core         []string
	suppressions []string
}

// resolveSupplyChainImpactFactIDs runs every branch whose inputs are
// non-empty and returns the deduplicated IDs in load order.
func (s FactStore) resolveSupplyChainImpactFactIDs(
	ctx context.Context,
	queryer db.Queryer,
	filter supplychaincore.SupplyChainImpactFactFilter,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) (supplyChainImpactResolvedIDs, error) {
	values := supplyChainImpactParamValues(filter)
	for _, resolution := range []struct {
		repositories        []string
		scopes, generations factimpact.Param
	}{
		{filter.RepositoryIDs, factimpact.RepositoryScopeIDs, factimpact.RepositoryGenerationIDs},
		{filter.FileRepositoryIDs, factimpact.FileRepositoryScopeIDs, factimpact.FileRepositoryGenerationIDs},
	} {
		scopes, generations, err := s.resolveSupplyChainImpactScopes(ctx, queryer, resolution.repositories, stats)
		if err != nil {
			return supplyChainImpactResolvedIDs{}, err
		}
		values[resolution.scopes], values[resolution.generations] = scopes, generations
	}
	seen := map[string]struct{}{}
	var resolved supplyChainImpactResolvedIDs
	for _, branch := range factimpact.Branches() {
		if !supplyChainImpactBranchRuns(branch, values) {
			continue
		}
		started := time.Now()
		rows, err := supplyChainImpactBranchRows(ctx, queryer, branch, values)
		if err != nil {
			return supplyChainImpactResolvedIDs{}, err
		}
		stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
			Branch: branch.Name, Rows: len(rows), Duration: time.Since(started),
		})
		if branch.Shape == factimpact.ShapeGuard {
			stats.GuardRows += len(rows)
			for _, row := range rows {
				if !slices.Contains(stats.GuardKinds, row.kind) {
					stats.GuardKinds = append(stats.GuardKinds, row.kind)
				}
			}
			slices.Sort(stats.GuardKinds)
		}
		for _, row := range rows {
			isSuppression := row.kind == chain.VulnerabilitySuppressionFactKind
			if isSuppression != branch.ReturnsSuppressions {
				return supplyChainImpactResolvedIDs{}, fmt.Errorf(
					"list active supply chain impact facts: branch %s returned %s fact %q", branch.Name, row.kind, row.id)
			}
			if row.kind == supplyChainImpactContainerImageIdentityFactKind {
				return supplyChainImpactResolvedIDs{}, fmt.Errorf(
					"list active supply chain impact facts: branch %s returned identity fact %q", branch.Name, row.id)
			}
			if _, duplicate := seen[row.id]; duplicate {
				continue
			}
			seen[row.id] = struct{}{}
			if isSuppression {
				resolved.suppressions = append(resolved.suppressions, row.id)
			} else {
				resolved.core = append(resolved.core, row.id)
			}
		}
	}
	byteOrder := func(left, right string) int { return bytes.Compare([]byte(left), []byte(right)) }
	slices.SortFunc(resolved.core, byteOrder)
	slices.SortFunc(resolved.suppressions, byteOrder)
	if len(resolved.suppressions) > maxSupplyChainImpactActiveEvidenceRowsPerCall {
		resolved.suppressions = resolved.suppressions[:maxSupplyChainImpactActiveEvidenceRowsPerCall]
		stats.SuppressionTruncated = true
	}
	stats.ResolvedFactIDs = len(resolved.core) + len(resolved.suppressions)
	return resolved, nil
}

// supplyChainImpactParamValues maps every catalog input to its bound list.
func supplyChainImpactParamValues(filter supplychaincore.SupplyChainImpactFactFilter) map[factimpact.Param]any {
	return map[factimpact.Param]any{
		factimpact.PackageIDs:          filter.PackageIDs,
		factimpact.PURLs:               filter.PURLs,
		factimpact.CVEIDs:              filter.CVEIDs,
		factimpact.AdvisoryIDs:         filter.AdvisoryIDs,
		factimpact.SubjectDigests:      filter.SubjectDigests,
		factimpact.ProductCriteria:     filter.ProductCriteria,
		factimpact.DocumentIDs:         filter.DocumentIDs,
		factimpact.RepositoryIDs:       filter.RepositoryIDs,
		factimpact.ImageRefs:           filter.ImageRefs,
		factimpact.FileRepositoryIDs:   filter.FileRepositoryIDs,
		factimpact.LowerPackageIDs:     lowerCleanedStringFilterValues(filter.PackageIDs),
		factimpact.LowerPURLs:          lowerCleanedStringFilterValues(filter.PURLs),
		factimpact.LowerCVEIDs:         lowerCleanedStringFilterValues(filter.CVEIDs),
		factimpact.LowerSubjectDigests: lowerCleanedStringFilterValues(filter.SubjectDigests),
		factimpact.LowerRepositoryIDs:  lowerCleanedStringFilterValues(filter.RepositoryIDs),
		factimpact.LowerAdvisoryIDs:    lowerCleanedStringFilterValues(filter.AdvisoryIDs),
		factimpact.SuppressionLimit:    maxSupplyChainImpactActiveEvidenceRowsPerCall + 1,
	}
}

// supplyChainImpactBranchRuns reports whether any input gating the branch is
// a non-empty list. A branch whose gating lists are all empty matches nothing.
func supplyChainImpactBranchRuns(branch factimpact.Branch, values map[factimpact.Param]any) bool {
	for _, param := range branch.RunWhen {
		if list, ok := values[param].([]string); ok && len(list) > 0 {
			return true
		}
	}
	return false
}

type supplyChainImpactBranchRow struct{ id, kind string }

func supplyChainImpactBranchRows(
	ctx context.Context,
	queryer db.Queryer,
	branch factimpact.Branch,
	values map[factimpact.Param]any,
) ([]supplyChainImpactBranchRow, error) {
	args := make([]any, 0, len(branch.Params))
	for _, param := range branch.Params {
		args = append(args, values[param])
	}
	rows, err := queryer.QueryContext(ctx, branch.SQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: branch %s: %w", branch.Name, err)
	}
	defer func() { _ = rows.Close() }()
	var out []supplyChainImpactBranchRow
	for rows.Next() {
		var row supplyChainImpactBranchRow
		if err := rows.Scan(&row.id, &row.kind); err != nil {
			return nil, fmt.Errorf("list active supply chain impact facts: branch %s: %w", branch.Name, err)
		}
		if row.id == "" {
			return nil, fmt.Errorf("list active supply chain impact facts: branch %s returned an empty fact ID", branch.Name)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: branch %s: %w", branch.Name, err)
	}
	return out, nil
}

// resolveSupplyChainImpactScopes returns the active scope IDs a repository
// list names through the scope arms, with each scope's active generation ID
// at the same position; an empty list issues no statement.
func (s FactStore) resolveSupplyChainImpactScopes(
	ctx context.Context,
	queryer db.Queryer,
	repositoryIDs []string,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) ([]string, []string, error) {
	if len(repositoryIDs) == 0 {
		return nil, nil, nil
	}
	started := time.Now()
	rows, err := queryer.QueryContext(ctx, factimpact.ScopeResolutionSQL, repositoryIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("list active supply chain impact facts: resolve scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes, generations []string
	for rows.Next() {
		var scopeID, generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			return nil, nil, fmt.Errorf("list active supply chain impact facts: resolve scopes: %w", err)
		}
		scopes = append(scopes, scopeID)
		generations = append(generations, generationID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list active supply chain impact facts: resolve scopes: %w", err)
	}
	stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
		Branch: supplychaincore.SupplyChainImpactActiveEvidenceScopeResolution, Rows: len(scopes), Duration: time.Since(started),
	})
	return scopes, generations, nil
}

func normalizeSupplyChainImpactFactFilter(filter *supplychaincore.SupplyChainImpactFactFilter) {
	filter.PackageIDs = cleanStringFilterValues(filter.PackageIDs)
	filter.PURLs = cleanStringFilterValues(filter.PURLs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.AdvisoryIDs = cleanStringFilterValues(filter.AdvisoryIDs)
	filter.SubjectDigests = cleanStringFilterValues(filter.SubjectDigests)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	filter.ProductCriteria = cleanStringFilterValues(filter.ProductCriteria)
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.FileRepositoryIDs = cleanStringFilterValues(filter.FileRepositoryIDs)
	filter.ImageRefs = cleanStringFilterValues(filter.ImageRefs)
}

func supplyChainImpactFactFilterEmpty(filter supplychaincore.SupplyChainImpactFactFilter) bool {
	return len(filter.PackageIDs) == 0 && len(filter.PURLs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.AdvisoryIDs) == 0 && len(filter.SubjectDigests) == 0 &&
		len(filter.DocumentIDs) == 0 && len(filter.ProductCriteria) == 0 &&
		len(filter.RepositoryIDs) == 0 && len(filter.FileRepositoryIDs) == 0 &&
		len(filter.ImageRefs) == 0
}
