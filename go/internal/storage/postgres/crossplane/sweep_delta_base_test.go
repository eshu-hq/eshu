// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// legOrderLog records every discovery call the sweep makes, in order.
type legOrderLog struct{ calls []string }

func (l *legOrderLog) add(format string, args ...any) {
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

// legOrderRows serves fixed rows to Scan.
type legOrderRows struct {
	rows [][]any
	at   int
}

func (r *legOrderRows) Next() bool { r.at++; return r.at <= len(r.rows) }
func (r *legOrderRows) Err() error { return nil }
func (r *legOrderRows) Close() error {
	return nil
}

func (r *legOrderRows) Scan(dest ...any) error {
	row := r.rows[r.at-1]
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = row[i].(string)
		case *bool:
			*d = row[i].(bool)
		default:
			return fmt.Errorf("unsupported scan target %T", d)
		}
	}
	return nil
}

// legOrderDB answers the XRD-still-active fence and the active discovery
// text: activeTargets[group] is every matching scope id, served in keyset
// pages after the cursor.
type legOrderDB struct {
	log           *legOrderLog
	activeTargets map[string][]string
}

func (d legOrderDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	switch query {
	case isCrossplaneXRDGenerationStillActiveQuery:
		return &legOrderRows{rows: [][]any{{true}}}, nil
	case ListCrossplaneRedriveTargetScopesQuery:
		group, after, limit := args[0].(string), args[3].(string), args[4].(int)
		d.log.add("active %s after=%q", group, after)
		return &legOrderRows{rows: keysetPage(d.activeTargets[group], after, limit, "-active")}, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

// legOrderDeltaBase is a fake delta-base collaborator.
type legOrderDeltaBase struct {
	log             *legOrderLog
	baseGenerations []string
	baseTargets     map[string][]string
}

func (b legOrderDeltaBase) CrossplaneRedriveDeltaBaseGenerations(context.Context) ([]string, error) {
	b.log.add("lookup")
	return b.baseGenerations, nil
}

func (b legOrderDeltaBase) ListCrossplaneRedriveDeltaBaseTargetScopes(
	_ context.Context, page CrossplaneRedriveTargetPage, baseGenerations []string,
) ([]CrossplaneRedriveTargetScope, error) {
	b.log.add("base %s after=%q generations=%v", page.Group, page.AfterScopeID, baseGenerations)
	var out []CrossplaneRedriveTargetScope
	for _, row := range keysetPage(b.baseTargets[page.Group], page.AfterScopeID, page.Limit, "-delta") {
		out = append(out, CrossplaneRedriveTargetScope{ScopeID: row[0].(string), GenerationID: row[1].(string)})
	}
	return out, nil
}

// keysetPage returns up to limit (scope, scope+suffix) rows after the cursor.
func keysetPage(scopes []string, after string, limit int, suffix string) [][]any {
	var rows [][]any
	for _, scopeID := range scopes {
		if scopeID > after && len(rows) < limit {
			rows = append(rows, []any{scopeID, scopeID + suffix})
		}
	}
	return rows
}

// legOrderReplayer records every scheduled target.
type legOrderReplayer struct{ scheduled []string }

func (r *legOrderReplayer) ReplayCrossplaneSatisfiedByMaterialization(_ context.Context, scopeID, generationID string) (bool, error) {
	r.scheduled = append(r.scheduled, scopeID+"@"+generationID)
	return true, nil
}

// TestRunLegsReadsDeltaGenerationsOnceAfterEveryActivePage pins the #10094
// leg order: the active leg pages every join key to its end, then the
// delta-base generation lookup runs once, then the base leg pages every key.
// A lookup that ran before an active page could miss a scope that turned
// delta-active through a delta touching no K8s file, which derives no intent
// of its own.
func TestRunLegsReadsDeltaGenerationsOnceAfterEveryActivePage(t *testing.T) {
	t.Parallel()
	log := &legOrderLog{}
	replayer := &legOrderReplayer{}
	sweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB: legOrderDB{log: log, activeTargets: map[string][]string{
			"a.example.org": {"s1", "s2"},
			"b.example.org": {"s3"},
		}},
		Replayer: replayer,
		DeltaBase: legOrderDeltaBase{log: log, baseGenerations: []string{"g2", "g4"}, baseTargets: map[string][]string{
			"a.example.org": {"s2", "s4"},
		}},
		PageSize: 2,
	}
	keys := []crossplaneRedriveXRDJoinKey{{group: "a.example.org", claimKind: "K"}, {group: "b.example.org", claimKind: "K"}}
	seen := map[string]struct{}{}
	result := CrossplaneRedriveSweepResult{}
	superseded, err := sweeper.runLegs(context.Background(), "xrd", "xrd-gen", keys, seen, &result)
	if err != nil || superseded {
		t.Fatalf("runLegs() = (%v, %v), want (false, nil)", superseded, err)
	}
	wantCalls := []string{
		`active a.example.org after=""`,
		`active a.example.org after="s2"`,
		`active b.example.org after=""`,
		"lookup",
		`base a.example.org after="" generations=[g2 g4]`,
		`base a.example.org after="s4" generations=[g2 g4]`,
		`base b.example.org after="" generations=[g2 g4]`,
	}
	if !slices.Equal(log.calls, wantCalls) {
		t.Fatalf("discovery calls =\n%q\nwant\n%q", log.calls, wantCalls)
	}
	// s2 is found by both legs and scheduled once, by the active leg.
	wantScheduled := []string{"s1@s1-active", "s2@s2-active", "s3@s3-active", "s4@s4-delta"}
	if !slices.Equal(replayer.scheduled, wantScheduled) {
		t.Fatalf("scheduled = %v, want %v", replayer.scheduled, wantScheduled)
	}
	if result.PagesProcessed != 6 || result.DeltaBasePagesProcessed != 3 || result.DeltaBaseTargetsEnqueued != 1 {
		t.Fatalf("result = %+v, want 6 pages, 3 delta-base pages, 1 delta-base target", result)
	}
}

// TestRunLegsWithoutDeltaGenerationsRunsNoBasePage pins the no-delta path: a
// nil collaborator runs no lookup, and an empty lookup runs no base page.
func TestRunLegsWithoutDeltaGenerationsRunsNoBasePage(t *testing.T) {
	t.Parallel()
	for name, deltaBase := range map[string]func(*legOrderLog) CrossplaneRedriveDeltaBaseTargetLister{
		"nil collaborator": func(*legOrderLog) CrossplaneRedriveDeltaBaseTargetLister { return nil },
		"no delta-active scope": func(log *legOrderLog) CrossplaneRedriveDeltaBaseTargetLister {
			return legOrderDeltaBase{log: log}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			log := &legOrderLog{}
			sweeper := CrossplaneSatisfiedByRedriveSweeper{
				DB:        legOrderDB{log: log, activeTargets: map[string][]string{"a.example.org": {"s1"}}},
				Replayer:  &legOrderReplayer{},
				DeltaBase: deltaBase(log),
			}
			result := CrossplaneRedriveSweepResult{}
			keys := []crossplaneRedriveXRDJoinKey{{group: "a.example.org", claimKind: "K"}}
			if _, err := sweeper.runLegs(context.Background(), "xrd", "xrd-gen", keys, map[string]struct{}{}, &result); err != nil {
				t.Fatalf("runLegs(): %v", err)
			}
			for _, call := range log.calls {
				if len(call) >= 4 && call[:4] == "base" {
					t.Fatalf("discovery calls = %q, want no base page", log.calls)
				}
			}
			if result.DeltaBasePagesProcessed != 0 || result.PagesProcessed != 1 {
				t.Fatalf("result = %+v, want one active page and no delta-base page", result)
			}
		})
	}
}
