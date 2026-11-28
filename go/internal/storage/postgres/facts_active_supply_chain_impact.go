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

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
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
		core, err := s.loadSupplyChainImpactPayloadPages(ctx, queryer, resolved.core, &stats, filter.PayloadBytesLimit)
		if err != nil {
			return err
		}
		if err := s.foldSupplyChainImpactFilePages(ctx, queryer, resolved.files, filter.FileFold, &stats); err != nil {
			return err
		}
		identity, err := s.loadSupplyChainImpactIdentityStream(ctx, queryer, filter, &stats)
		if err != nil {
			return err
		}
		suppressions, err := s.loadSupplyChainImpactPayloadPages(ctx, queryer, resolved.suppressions, &stats, filter.PayloadBytesLimit)
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
//
// files holds the parser file fact IDs a caller with a FileFold folds instead
// of loading; it stays empty when the filter carries no fold, and those IDs are
// then part of core.
type supplyChainImpactResolvedIDs struct {
	core         []string
	files        []string
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
	branches := factimpact.Branches()
	if s.impactStaticSnapshot {
		branches = factimpact.ProtectedStaticBranches()
	}
	for _, branch := range branches {
		bound := supplyChainImpactBranchValues(branch, values, filter)
		if !supplyChainImpactBranchRuns(branch, bound) {
			continue
		}
		rows, err := supplyChainImpactBranchStatements(ctx, queryer, branch, bound, stats)
		if err != nil {
			return supplyChainImpactResolvedIDs{}, err
		}
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
			} else if filter.FileFold != nil && row.kind == factload.FactKindFile {
				resolved.files = append(resolved.files, row.id)
			} else {
				resolved.core = append(resolved.core, row.id)
			}
		}
	}
	byteOrder := func(left, right string) int { return bytes.Compare([]byte(left), []byte(right)) }
	slices.SortFunc(resolved.core, byteOrder)
	slices.SortFunc(resolved.files, byteOrder)
	slices.SortFunc(resolved.suppressions, byteOrder)
	if len(resolved.suppressions) > maxSupplyChainImpactActiveEvidenceRowsPerCall {
		resolved.suppressions = resolved.suppressions[:maxSupplyChainImpactActiveEvidenceRowsPerCall]
		stats.SuppressionTruncated = true
	}
	stats.ResolvedFactIDs = len(resolved.core) + len(resolved.files) + len(resolved.suppressions)
	return resolved, nil
}

// supplyChainImpactBranchValues returns the bind map for one branch. Under the
// #11087 criteria bound, affected_product_criteria and foreign_key_guard read
// component-origin criteria only. Under the #11097 package bound,
// affected_package_package_id, affected_package_purl, and foreign_key_guard
// read owned/exempt PackageIDs and PURLs (and their lowered twins) only, so a
// seed vulnerability's package cannot pull unrelated vulnerabilities directly
// or re-enter through the guard's package_id/purl arms. Both rebinds compose
// cleanly on foreign_key_guard when active simultaneously. Every other branch
// keeps the union, and seam-off (all seed lists empty) returns the shared map
// unchanged. The copy keeps the shared map immutable for the other branches.
func supplyChainImpactBranchValues(branch factimpact.Branch, values map[factimpact.Param]any, filter supplychaincore.SupplyChainImpactFactFilter) map[factimpact.Param]any {
	rebindCriteria := len(filter.SeedProductCriteria) > 0 &&
		(branch.Name == factimpact.BranchAffectedProductCriteria || branch.Name == factimpact.BranchForeignKeyGuard)
	rebindPackages := (len(filter.SeedPackageIDs) > 0 || len(filter.SeedPURLs) > 0) &&
		(branch.Name == factimpact.BranchAffectedPackagePackageID ||
			branch.Name == factimpact.BranchAffectedPackagePURL ||
			branch.Name == factimpact.BranchForeignKeyGuard)
	if !rebindCriteria && !rebindPackages {
		return values
	}
	rebound := make(map[factimpact.Param]any, len(values)+1)
	for param, list := range values {
		rebound[param] = list
	}
	if rebindCriteria {
		rebound[factimpact.ProductCriteria] = filter.ProductCriteria
	}
	if rebindPackages {
		rebound[factimpact.PackageIDs] = filter.PackageIDs
		rebound[factimpact.PURLs] = filter.PURLs
		rebound[factimpact.LowerPackageIDs] = lowerCleanedStringFilterValues(filter.PackageIDs)
		rebound[factimpact.LowerPURLs] = lowerCleanedStringFilterValues(filter.PURLs)
	}
	return rebound
}

// supplyChainImpactParamValues maps every catalog input to its bound list.
//
// The criteria, package_id, and purl entries bind the UNION of owned and
// seed-CVE lists (#11087, #11097): consumer, anchor, and suppression branches
// keep the union, while the resolve loop rebinds affected_product_criteria,
// affected_package_package_id, affected_package_purl, and foreign_key_guard to
// the owned/exempt lists alone. With both seams off all seed lists are empty
// and every union equals its base list.
func supplyChainImpactParamValues(filter supplychaincore.SupplyChainImpactFactFilter) map[factimpact.Param]any {
	packageIDs := unionSupplyChainImpactStrings(filter.PackageIDs, filter.SeedPackageIDs)
	purls := unionSupplyChainImpactStrings(filter.PURLs, filter.SeedPURLs)
	return map[factimpact.Param]any{
		factimpact.PackageIDs:          packageIDs,
		factimpact.PURLs:               purls,
		factimpact.CVEIDs:              filter.CVEIDs,
		factimpact.AdvisoryIDs:         filter.AdvisoryIDs,
		factimpact.SubjectDigests:      filter.SubjectDigests,
		factimpact.ProductCriteria:     unionSupplyChainImpactStrings(filter.ProductCriteria, filter.SeedProductCriteria),
		factimpact.DocumentIDs:         filter.DocumentIDs,
		factimpact.RepositoryIDs:       filter.RepositoryIDs,
		factimpact.ImageRefs:           filter.ImageRefs,
		factimpact.FileRepositoryIDs:   filter.FileRepositoryIDs,
		factimpact.LowerPackageIDs:     lowerCleanedStringFilterValues(packageIDs),
		factimpact.LowerPURLs:          lowerCleanedStringFilterValues(purls),
		factimpact.LowerCVEIDs:         lowerCleanedStringFilterValues(filter.CVEIDs),
		factimpact.LowerSubjectDigests: lowerCleanedStringFilterValues(filter.SubjectDigests),
		factimpact.LowerRepositoryIDs:  lowerCleanedStringFilterValues(filter.RepositoryIDs),
		factimpact.LowerAdvisoryIDs:    lowerCleanedStringFilterValues(filter.AdvisoryIDs),
		factimpact.SuppressionLimit:    maxSupplyChainImpactActiveEvidenceRowsPerCall + 1,
		factimpact.UpstreamCVEIDs:      filter.UpstreamCVEIDs,
		factimpact.AliasIDs:            filter.AliasIDs,
		factimpact.LowerAliasIDs:       lowerCleanedStringFilterValues(filter.AliasIDs),
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

// supplyChainImpactBranchStatements runs one branch and records one stat per
// statement. A branch with a MaxListLength runs once per chunk of its bound list;
// any other runs once.
func supplyChainImpactBranchStatements(
	ctx context.Context,
	queryer db.Queryer,
	branch factimpact.Branch,
	values map[factimpact.Param]any,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) ([]supplyChainImpactBranchRow, error) {
	run := func(bound map[factimpact.Param]any) ([]supplyChainImpactBranchRow, error) {
		started := time.Now()
		rows, err := supplyChainImpactBranchRows(ctx, queryer, branch, bound)
		if err != nil {
			return nil, err
		}
		stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
			Branch: branch.Name, Rows: len(rows), Duration: time.Since(started),
		})
		return rows, nil
	}
	if branch.MaxListLength <= 0 {
		return run(values)
	}
	list, ok := values[branch.Params[0]].([]string)
	if !ok {
		return nil, fmt.Errorf("list active supply chain impact facts: branch %s binds non-list param %d", branch.Name, branch.Params[0])
	}
	var out []supplyChainImpactBranchRow
	for chunk := range slices.Chunk(list, branch.MaxListLength) {
		rows, err := run(map[factimpact.Param]any{branch.Params[0]: chunk})
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

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
	filter.SeedPackageIDs = cleanStringFilterValues(filter.SeedPackageIDs)
	filter.PURLs = cleanStringFilterValues(filter.PURLs)
	filter.SeedPURLs = cleanStringFilterValues(filter.SeedPURLs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.UpstreamCVEIDs = cleanStringFilterValues(filter.UpstreamCVEIDs)
	filter.AliasIDs = cleanStringFilterValues(filter.AliasIDs)
	filter.AdvisoryIDs = cleanStringFilterValues(filter.AdvisoryIDs)
	filter.SubjectDigests = cleanStringFilterValues(filter.SubjectDigests)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	filter.ProductCriteria = cleanStringFilterValues(filter.ProductCriteria)
	filter.SeedProductCriteria = cleanStringFilterValues(filter.SeedProductCriteria)
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.FileRepositoryIDs = cleanStringFilterValues(filter.FileRepositoryIDs)
	filter.ImageRefs = cleanStringFilterValues(filter.ImageRefs)
}

func supplyChainImpactFactFilterEmpty(filter supplychaincore.SupplyChainImpactFactFilter) bool {
	return len(filter.PackageIDs) == 0 && len(filter.SeedPackageIDs) == 0 &&
		len(filter.PURLs) == 0 && len(filter.SeedPURLs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.UpstreamCVEIDs) == 0 && len(filter.AliasIDs) == 0 && len(filter.AdvisoryIDs) == 0 && len(filter.SubjectDigests) == 0 &&
		len(filter.DocumentIDs) == 0 && len(filter.ProductCriteria) == 0 && len(filter.SeedProductCriteria) == 0 &&
		len(filter.RepositoryIDs) == 0 && len(filter.FileRepositoryIDs) == 0 &&
		len(filter.ImageRefs) == 0
}

// unionSupplyChainImpactStrings returns the sorted, de-duplicated union of an
// owned list and its seed-CVE twin (#11087, #11097). It never mutates either
// input.
func unionSupplyChainImpactStrings(owned, seed []string) []string {
	out := make([]string, 0, len(owned)+len(seed))
	out = append(out, owned...)
	out = append(out, seed...)
	slices.Sort(out)
	return slices.Compact(out)
}
