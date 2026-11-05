// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Read label values of eshu_dp_crossplane_redrive_targets_enqueued_total and
// eshu_dp_crossplane_redrive_pages_processed_total: the discovery leg that
// found a target or ran a page (#10094).
const (
	crossplaneRedriveReadActive    = "active"
	crossplaneRedriveReadDeltaBase = "delta_base"
)

// CrossplaneRedriveTargetScope is one discovered target Claim scope and the
// generation its SATISFIED_BY intent is scheduled against: the scope's
// active generation (for the delta-base leg, the active delta).
type CrossplaneRedriveTargetScope struct {
	ScopeID      string
	GenerationID string
}

// CrossplaneRedriveTargetPage is one keyset page request of target
// discovery for one XRD (group, claim kind) join key.
type CrossplaneRedriveTargetPage struct {
	Group        string
	ClaimKind    string
	XRDScopeID   string
	AfterScopeID string
	Limit        int
}

// QueryArgs returns the page's arguments in the placeholder order of
// ListCrossplaneRedriveTargetScopesQuery ($1 to $5), which every text
// derived from it shares.
func (p CrossplaneRedriveTargetPage) QueryArgs() []any {
	return []any{p.Group, p.ClaimKind, p.XRDScopeID, p.AfterScopeID, p.Limit}
}

// CrossplaneRedriveDeltaBaseTargetLister is the sweep's delta-base discovery
// collaborator (#10094). A claim that lives only in the base full
// generation of a delta-active scope (the delta did not touch its file) is
// invisible to the active leg, which joins only active generations.
// postgres.FactStore implements this interface by deriving the base leg
// from ListCrossplaneRedriveTargetScopesQuery with deriveDeltaOverlayBaseQuery,
// so the base predicate has one definition and this package never imports
// the parent postgres package.
//
// CrossplaneRedriveDeltaBaseGenerations returns the candidate base
// generation ids a base leg can reach: the activated full generations at
// the union-base commit of every delta-active scope with a recorded
// baseline (a legacy delta has no base). The sweep reads it once per
// fan-out, after every active leg.
// ListCrossplaneRedriveDeltaBaseTargetScopes runs one base page bound to
// those generations and returns each target with its active delta
// generation. Discovery applies no delta overlay: a scope whose delta
// deleted the claim is still returned, and the handler, which reads the
// overlaid snapshot, writes nothing for it.
type CrossplaneRedriveDeltaBaseTargetLister interface {
	CrossplaneRedriveDeltaBaseGenerations(ctx context.Context) ([]string, error)
	ListCrossplaneRedriveDeltaBaseTargetScopes(ctx context.Context, page CrossplaneRedriveTargetPage, baseGenerations []string) ([]CrossplaneRedriveTargetScope, error)
}

// QueryCrossplaneRedriveTargetScopes runs one target-discovery page and
// fully drains and closes its rows before returning, so a caller never holds
// the SELECT's connection open while it issues a write. label prefixes
// every error.
func QueryCrossplaneRedriveTargetScopes(
	ctx context.Context,
	queryer db.Queryer,
	label string,
	query string,
	args ...any,
) ([]CrossplaneRedriveTargetScope, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = rows.Close() }()

	var page []CrossplaneRedriveTargetScope
	for rows.Next() {
		var target CrossplaneRedriveTargetScope
		if scanErr := rows.Scan(&target.ScopeID, &target.GenerationID); scanErr != nil {
			return nil, fmt.Errorf("%s: scan: %w", label, scanErr)
		}
		page = append(page, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return page, nil
}

// crossplaneRedriveLeg is one discovery leg: its read label and its page
// fetch.
type crossplaneRedriveLeg struct {
	read  string
	fetch func(ctx context.Context, page CrossplaneRedriveTargetPage) ([]CrossplaneRedriveTargetScope, error)
}

// activeLeg pages ListCrossplaneRedriveTargetScopesQuery on the sweeper's DB.
func (s CrossplaneSatisfiedByRedriveSweeper) activeLeg() crossplaneRedriveLeg {
	return crossplaneRedriveLeg{
		read: crossplaneRedriveReadActive,
		fetch: func(ctx context.Context, page CrossplaneRedriveTargetPage) ([]CrossplaneRedriveTargetScope, error) {
			return QueryCrossplaneRedriveTargetScopes(ctx, s.DB, "list crossplane redrive target scopes",
				ListCrossplaneRedriveTargetScopesQuery, page.QueryArgs()...)
		},
	}
}

// deltaBaseLeg pages the DeltaBase collaborator bound to baseGenerations.
func (s CrossplaneSatisfiedByRedriveSweeper) deltaBaseLeg(baseGenerations []string) crossplaneRedriveLeg {
	return crossplaneRedriveLeg{
		read: crossplaneRedriveReadDeltaBase,
		fetch: func(ctx context.Context, page CrossplaneRedriveTargetPage) ([]CrossplaneRedriveTargetScope, error) {
			targets, err := s.DeltaBase.ListCrossplaneRedriveDeltaBaseTargetScopes(ctx, page, baseGenerations)
			if err != nil {
				return nil, fmt.Errorf("list crossplane redrive delta base target scopes: %w", err)
			}
			return targets, nil
		},
	}
}

// runLegs runs the fan-out's discovery legs for every join key in order:
// the active leg for every key, then (with a DeltaBase collaborator) one
// delta-active scope lookup, then the delta-base leg for every key. Each
// leg pages with its own cursor; seenTargets dedupes across legs and keys.
//
// The lookup runs after the last active page of every key. A scope that was
// delta-active when its active page ran was delta-active before the lookup,
// so the base leg reaches it. A scope that turns delta-active later was
// full-active when its active page ran, so the active leg already scheduled
// it. Reading the lookup first would miss a scope that turns delta-active
// through a delta touching no K8s file: such a delta derives no SATISFIED_BY
// intent of its own (the projector builds intents only from a generation's
// own facts). It reports superseded when the XRD generation fence fired.
func (s CrossplaneSatisfiedByRedriveSweeper) runLegs(
	ctx context.Context,
	xrdScopeID string,
	xrdGenerationID string,
	keys []crossplaneRedriveXRDJoinKey,
	seenTargets map[string]struct{},
	result *CrossplaneRedriveSweepResult,
) (bool, error) {
	active := s.activeLeg()
	for _, key := range keys {
		superseded, err := s.sweepJoinKey(ctx, xrdScopeID, xrdGenerationID, key, active, seenTargets, result)
		if err != nil || superseded {
			return superseded, err
		}
	}
	if s.DeltaBase == nil {
		return false, nil
	}
	baseGenerations, err := s.DeltaBase.CrossplaneRedriveDeltaBaseGenerations(ctx)
	if err != nil {
		return false, fmt.Errorf("read crossplane redrive delta base generations: %w", err)
	}
	if len(baseGenerations) == 0 {
		// No candidate base generation (no delta-active scope with an
		// activated full generation at its union-base commit): no base
		// statement runs.
		return false, nil
	}
	base := s.deltaBaseLeg(baseGenerations)
	for _, key := range keys {
		superseded, err := s.sweepJoinKey(ctx, xrdScopeID, xrdGenerationID, key, base, seenTargets, result)
		if err != nil || superseded {
			return superseded, err
		}
	}
	return false, nil
}

// recordTargetsAndPages records the fan-out's targets and pages per read
// leg. Nil instruments are a no-op.
func (s CrossplaneSatisfiedByRedriveSweeper) recordTargetsAndPages(ctx context.Context, result CrossplaneRedriveSweepResult) {
	if s.Instruments == nil {
		return
	}
	legs := []struct {
		read          string
		targets, page int
	}{
		{crossplaneRedriveReadActive, result.TargetsEnqueued - result.DeltaBaseTargetsEnqueued, result.PagesProcessed - result.DeltaBasePagesProcessed},
		{crossplaneRedriveReadDeltaBase, result.DeltaBaseTargetsEnqueued, result.DeltaBasePagesProcessed},
	}
	for _, leg := range legs {
		attrs := metric.WithAttributes(telemetry.AttrRead(leg.read))
		if s.Instruments.CrossplaneRedriveTargetsEnqueued != nil && leg.targets > 0 {
			s.Instruments.CrossplaneRedriveTargetsEnqueued.Add(ctx, int64(leg.targets), attrs)
		}
		if s.Instruments.CrossplaneRedrivePagesProcessed != nil && leg.page > 0 {
			s.Instruments.CrossplaneRedrivePagesProcessed.Add(ctx, int64(leg.page), attrs)
		}
	}
}
