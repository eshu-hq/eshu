// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// ListActivePackageManifestDependencyFacts loads active, non-tombstoned Git
// manifest dependency entities for the specific ecosystem/name set requested by
// a package-registry reducer intent.
//
// A scope whose active generation is a git delta reads its delta overlaid on
// its #7797 base full generation (#9991): base rows keep their own generation
// id, and the delta wins per stable key, by tombstone, and by touched path.
// A refused, legacy, or mismatched base keeps the delta-only rows; see
// overlayDeltaBase. Every statement runs on one read-only
// repeatable-read snapshot.
// Performance Evidence: docs/internal/evidence/9991-manifest-read-delta-union.md.
//
// The query filters fact.is_tombstone = FALSE so a manifest dependency fact
// tombstoned within a still-active generation is never surfaced as live. The
// active generation pointer keeps tombstoned rows joinable, so the predicate is
// the only guard against returning superseded dependency entities to
// correlation consumers; the package-source and security-alert reconciliation
// handlers do not filter IsTombstone themselves. This matches every sibling
// active source-local reader.
//
// The dependency indexes do not cover is_tombstone, so the predicate is a
// residual filter on the rows each index probe already visits (#1927).
//
// Each requested (package manager, folded name) pair is its own probe of the
// folded entity index (#9611); see listActivePackageManifestDependencyFactsQuery.
// The keyset cursor, ordering, page size, and statements per read are
// unchanged. Performance Evidence:
// docs/internal/evidence/9611-manifest-read-name-probe.md.
//
// Case-Fold Evidence (#9568): the entity_name prefilter folds the stored name
// with LOWER(TRIM(entity_name) COLLATE "C"). The LOWER under COLLATE "C" is
// byte-identical to Go packageidentity.LowerASCII; TRIM strips spaces, which
// matches how the $2 candidate set is trimmed for real parser output (Go
// strings.TrimSpace would also strip other unicode whitespace, which parsers
// never emit in a name). Manifests store registry case (NuGet Newtonsoft.Json,
// pypi PyYAML). The $2 candidate set folds the same way, and the key join in
// packageidentity.ManifestConsumptionKeys plus the join/cursor/ordering shape
// are unchanged, so the key join stays the accuracy gate.
// Migration 247 adds the mirrored
// fact_records_active_package_dependency_folded_entity_idx expression index;
// on a disposable PostgreSQL 18.6 fixture the folded read uses an index scan
// with no sequential scan in every measured shape, and a selective name seeks
// the new index in 1.2-1.7 ms versus 16-21 ms through the old prefix.
// No-Observability-Change: the existing read_seconds/read_pages metrics cover
// this read; the impact handler's manifest_dependency_facts count goes from 0
// to the matched row count for mixed-case consumers.
// Observability Evidence: each completed read records its wall time and page
// count in eshu_dp_package_manifest_dependency_read_seconds and
// eshu_dp_package_manifest_dependency_read_pages, labeled read (#9586).
func (s FactStore) ListActivePackageManifestDependencyFacts(
	ctx context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	started := time.Now()
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	if len(ecosystems) == 0 || len(packageNames) == 0 {
		return nil, nil
	}

	packageManagers, query := activePackageManifestDependencyQuery(ecosystems)
	return s.listManifestDependencyFactsWithQuery(ctx, started, packageManagers, query, packageNames)
}

// listManifestDependencyFactsWithQuery runs one manifest dependency read for
// the active query text query: its active pages, the delta-target lookup, the
// base pages of the text derived from query, and the delta overlay, all on one
// snapshot. ListActivePackageManifestDependencyFacts picks query; the #9611
// parity proof passes the pre-#9611 texts through the same path.
func (s FactStore) listManifestDependencyFactsWithQuery(
	ctx context.Context,
	started time.Time,
	packageManagers []string,
	query string,
	packageNames []string,
) ([]facts.Envelope, error) {
	// The read label names the query text that ran: keep the pre-transform
	// text, because on a snapshot clone the static rewrite below changes
	// the text and no rewritten text is in the label table.
	rawQuery := query
	baseQuery, err := deriveDeltaOverlayBaseQuery(query, manifestDeltaOverlayScopeParam)
	if err != nil {
		return nil, err
	}
	if query, err = s.protectedImpactStaticFactSQL(query); err != nil {
		return nil, err
	}
	if baseQuery, err = s.protectedImpactStaticFactSQL(baseQuery); err != nil {
		return nil, err
	}
	// One read-only repeatable-read snapshot keeps the active pages, the
	// delta-target lookup, the delta-base pages, the base resolution, and the
	// overlay inputs on the same active generations (#9991). Inside the
	// impact snapshot this nests as a no-op.
	var loaded []facts.Envelope
	activeRows := 0
	sink := s.manifestDeltaOverlayTelemetry(manifestDependencyReadLabel(rawQuery))
	err = s.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		reader := snapshot.store
		active, err := reader.listPackageManifestDependencyFactPages(ctx, query, packageManagers, packageNames)
		if err != nil {
			return err
		}
		activeRows = len(active)
		targets, err := snapshot.deltaTargets(ctx)
		if err != nil {
			reader.recordDeltaOverlayFailure(ctx, sink)
			return err
		}
		var base []facts.Envelope
		if baseGenerations, scan := deltaOverlayBaseGenerations(targets); scan {
			base, err = reader.listPackageManifestDependencyFactPages(ctx, baseQuery, packageManagers, packageNames, baseGenerations)
			if err != nil {
				reader.recordDeltaOverlayFailure(ctx, sink)
				return err
			}
		}
		loaded, err = reader.overlayDeltaBase(ctx, sink, targets, active, base)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.recordManifestDependencyRead(ctx, rawQuery, started, activeRows)
	return loaded, nil
}

// listPackageManifestDependencyFactPages runs one manifest dependency query
// text through every keyset page. extra binds as $6 onward on every page.
func (s FactStore) listPackageManifestDependencyFactPages(
	ctx context.Context,
	query string,
	packageManagers []string,
	packageNames []string,
	extra ...any,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActivePackageManifestDependencyFactsPage(
			ctx,
			query,
			packageManagers,
			packageNames,
			cursorObservedAt,
			cursorFactID,
			extra...,
		)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listActivePackageManifestDependencyFactsPage(
	ctx context.Context,
	query string,
	packageManagers []string,
	packageNames []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	extra ...any,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	args := append([]any{
		manifestDependencyQueryExecMode,
		packageManagers,
		packageNames,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	}, extra...)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active package manifest dependency facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active package manifest dependency facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active package manifest dependency facts: %w", err)
	}
	return loaded, nil
}

// activePackageManifestDependencyQuery returns the package_manager values and
// the query one ListActivePackageManifestDependencyFacts call uses. Each
// ecosystem expands to the parser package_manager values it can represent.
// Maven selects the sbt-aware query; other ecosystems use the original query.
func activePackageManifestDependencyQuery(ecosystems []string) ([]string, string) {
	hasMaven := slices.ContainsFunc(ecosystems, func(ecosystem string) bool {
		return packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem)) == packageidentity.EcosystemMaven
	})
	expanded := make([]string, 0, len(ecosystems)+2)
	for _, ecosystem := range ecosystems {
		expanded = append(expanded, packageidentity.ManifestPackageManagersForEcosystem(ecosystem)...)
	}
	slices.Sort(expanded)
	expanded = slices.Compact(expanded)
	if !hasMaven {
		return expanded, listActivePackageManifestDependencyFactsQuery
	}
	return expanded, listActivePackageManifestDependencyFactsWithSBTQuery
}
