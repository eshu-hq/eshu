// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestCodeReachabilityProjectionRunnerProcessesPendingInputs(t *testing.T) {
	t.Parallel()

	loader := &fakeCodeReachabilityInputLoader{
		inputs: []CodeReachabilityProjectionInput{{
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
			RepositoryID: "repo-1",
			Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
			Edges: []CodeReachabilityEdge{{
				SourceEntityID:   "entity:root",
				TargetEntityID:   "entity:leaf",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			}},
			MaxDepth:  5,
			UpdatedAt: time.Date(2026, 6, 17, 3, 59, 0, 0, time.UTC),
		}},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config: CodeReachabilityProjectionRunnerConfig{
			BatchLimit: 10,
		},
	}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if got, want := result.InputsProcessed, 1; got != want {
		t.Fatalf("InputsProcessed = %d, want %d", got, want)
	}
	if got, want := result.RowsWritten, 2; got != want {
		t.Fatalf("RowsWritten = %d, want %d rows=%#v", got, want, writer.rows)
	}
	if writer.scopeID != "scope-1" || writer.generationID != "generation-1" || writer.repositoryID != "repo-1" {
		t.Fatalf("replacement keys = %q/%q/%q, want scope-1/generation-1/repo-1", writer.scopeID, writer.generationID, writer.repositoryID)
	}
	if got, want := writer.watermark, loader.inputs[0].UpdatedAt; !got.Equal(want) {
		t.Fatalf("watermark = %v, want loaded completed_at %v", got, want)
	}
	if got, want := writer.rows[1].EntityID, "entity:leaf"; got != want {
		t.Fatalf("written leaf entity = %q, want %q", got, want)
	}
}

// TestCodeReachabilityProjectionRunnerExcludesDowngradedControllerFromBFS is the
// #5376 runner harness proof: a mis-based *Controller whose superclass resolves
// repo-wide onward to a non-controller reject branch is (a) recorded as a
// downgraded verdict, and (b) removed from the BFS root set BEFORE reachability
// is materialized, so a helper reachable ONLY from that fake controller action
// gets no reachability row. A correctly-based controller's action stays a root
// and its uniquely-reached helper is still materialized.
func TestCodeReachabilityProjectionRunnerExcludesDowngradedControllerFromBFS(t *testing.T) {
	t.Parallel()

	loader := &fakeCodeReachabilityInputLoader{
		inputs: []CodeReachabilityProjectionInput{{
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
			RepositoryID: "repo-1",
			Roots: []CodeReachabilityRoot{
				{EntityID: "fn:GoodController:index", RootKinds: []string{CodeRootKindRubyRailsControllerAction}, ClassContext: "GoodController"},
				{EntityID: "fn:FakeController:index", RootKinds: []string{CodeRootKindRubyRailsControllerAction}, ClassContext: "FakeController"},
			},
			RubyClasses: []RubyClassEntity{
				{Name: "GoodController", QualifiedBases: []string{"ApplicationController"}},
				{Name: "FakeController", QualifiedBases: []string{"ApplicationRecord"}},
				{Name: "ApplicationRecord", QualifiedBases: []string{"ActiveRecord::Base"}},
			},
			Edges: []CodeReachabilityEdge{
				{SourceEntityID: "fn:GoodController:index", TargetEntityID: "fn:good_helper", RelationshipType: "CALLS", ResolutionMethod: "scip"},
				{SourceEntityID: "fn:FakeController:index", TargetEntityID: "fn:fake_helper", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			},
			MaxDepth:  5,
			UpdatedAt: time.Date(2026, 7, 20, 3, 59, 0, 0, time.UTC),
		}},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{InputLoader: loader, RowWriter: writer, Config: CodeReachabilityProjectionRunnerConfig{BatchLimit: 10}}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}

	// Both controller actions get a verdict row (one confirmed, one downgraded).
	if got, want := result.VerdictsWritten, 2; got != want {
		t.Fatalf("VerdictsWritten = %d, want %d (%#v)", got, want, writer.verdicts)
	}
	if got, want := result.VerdictsDowngraded, 1; got != want {
		t.Fatalf("VerdictsDowngraded = %d, want %d", got, want)
	}
	downgradedFake := false
	for _, v := range writer.verdicts {
		if v.EntityID == "fn:FakeController:index" && v.Verdict == CodeRootVerdictDowngraded {
			downgradedFake = true
		}
		if v.EntityID == "fn:GoodController:index" && v.Verdict != CodeRootVerdictConfirmed {
			t.Fatalf("GoodController action must be confirmed, got %q", v.Verdict)
		}
	}
	if !downgradedFake {
		t.Fatalf("FakeController action must be downgraded, verdicts=%#v", writer.verdicts)
	}

	// Reachability: the good helper (reached from the confirmed root) is
	// materialized; the fake helper (reached only from the downgraded, removed
	// root) is NOT.
	reachable := map[string]bool{}
	for _, r := range writer.rows {
		reachable[r.EntityID] = true
	}
	if !reachable["fn:good_helper"] {
		t.Fatalf("good_helper reachable from a confirmed controller must be materialized, rows=%#v", writer.rows)
	}
	if reachable["fn:fake_helper"] {
		t.Fatalf("fake_helper is only reachable from a downgraded controller root and must NOT be materialized")
	}
	if reachable["fn:FakeController:index"] {
		t.Fatalf("downgraded controller action must not appear as its own reachability root")
	}
}

// TestCodeReachabilityProjectionRunnerExcludesRouteUnreachableControllerFromBFS
// is the #5494 runner harness proof: an ancestry-confirmed controller action
// with NO backing route, in a repo whose route surface is exact-only and
// proven observed, is (a) recorded as a route-downgraded verdict via the
// SAME BuildCodeRootVerdicts + ReplaceRepositoryRows path #5376 uses, and (b)
// removed from the BFS root set before reachability is materialized, so a
// helper reachable ONLY from it gets no reachability row. A routed sibling
// action stays a root and its uniquely-reached helper is still materialized.
func TestCodeReachabilityProjectionRunnerExcludesRouteUnreachableControllerFromBFS(t *testing.T) {
	t.Parallel()

	loader := &fakeCodeReachabilityInputLoader{
		inputs: []CodeReachabilityProjectionInput{{
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
			RepositoryID: "repo-1",
			Roots: []CodeReachabilityRoot{
				{EntityID: "fn:OrdersController:index", RootKinds: []string{CodeRootKindRubyRailsControllerAction}, ClassContext: "OrdersController", ActionName: "index"},
				{EntityID: "fn:OrdersController:orphan", RootKinds: []string{CodeRootKindRubyRailsControllerAction}, ClassContext: "OrdersController", ActionName: "orphan"},
			},
			RubyClasses: []RubyClassEntity{
				{Name: "OrdersController", QualifiedBases: []string{"ApplicationController"}},
			},
			RubyRoutes: RubyRailsRouteFacts{
				HasAnyRouteEvidence: true,
				RoutedHandlers:      map[string]struct{}{"OrdersController.index": {}},
			},
			Edges: []CodeReachabilityEdge{
				{SourceEntityID: "fn:OrdersController:index", TargetEntityID: "fn:routed_helper", RelationshipType: "CALLS", ResolutionMethod: "scip"},
				{SourceEntityID: "fn:OrdersController:orphan", TargetEntityID: "fn:orphan_helper", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			},
			MaxDepth:  5,
			UpdatedAt: time.Date(2026, 7, 22, 3, 59, 0, 0, time.UTC),
		}},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{InputLoader: loader, RowWriter: writer, Config: CodeReachabilityProjectionRunnerConfig{BatchLimit: 10}}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}

	if got, want := result.VerdictsWritten, 2; got != want {
		t.Fatalf("VerdictsWritten = %d, want %d (%#v)", got, want, writer.verdicts)
	}
	if got, want := result.VerdictsDowngraded, 1; got != want {
		t.Fatalf("VerdictsDowngraded = %d, want %d", got, want)
	}
	if got, want := result.VerdictsRouteDowngraded, 1; got != want {
		t.Fatalf("VerdictsRouteDowngraded = %d, want %d", got, want)
	}

	downgradedOrphan := false
	for _, v := range writer.verdicts {
		if v.EntityID == "fn:OrdersController:orphan" {
			if v.Verdict != CodeRootVerdictDowngraded || v.Basis.Reason != ReasonRouteUnreachable {
				t.Fatalf("orphan action must be route-downgraded, got %+v", v)
			}
			downgradedOrphan = true
		}
		if v.EntityID == "fn:OrdersController:index" && v.Verdict != CodeRootVerdictConfirmed {
			t.Fatalf("routed index action must be confirmed, got %q", v.Verdict)
		}
	}
	if !downgradedOrphan {
		t.Fatalf("orphan action must appear as a downgraded verdict, verdicts=%#v", writer.verdicts)
	}

	reachable := map[string]bool{}
	for _, r := range writer.rows {
		reachable[r.EntityID] = true
	}
	if !reachable["fn:routed_helper"] {
		t.Fatalf("routed_helper reachable from the confirmed routed action must be materialized, rows=%#v", writer.rows)
	}
	if reachable["fn:orphan_helper"] {
		t.Fatalf("orphan_helper is only reachable from a route-downgraded root and must NOT be materialized")
	}
	if reachable["fn:OrdersController:orphan"] {
		t.Fatalf("route-downgraded controller action must not appear as its own reachability root")
	}
}

func TestCodeReachabilityProjectionRunnerRecordsEmptyInputWatermark(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 6, 17, 4, 5, 0, 0, time.UTC)
	loader := &fakeCodeReachabilityInputLoader{
		inputs: []CodeReachabilityProjectionInput{{
			ScopeID:      "scope-empty",
			GenerationID: "generation-empty",
			RepositoryID: "repo-empty",
			UpdatedAt:    completedAt,
		}},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
	}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 6, 17, 4, 10, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if got, want := result.InputsProcessed, 1; got != want {
		t.Fatalf("InputsProcessed = %d, want %d", got, want)
	}
	if got, want := result.RowsWritten, 0; got != want {
		t.Fatalf("RowsWritten = %d, want %d", got, want)
	}
	if got, want := writer.watermark, completedAt; !got.Equal(want) {
		t.Fatalf("watermark = %v, want loaded completed_at %v", got, want)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("rows = %#v, want empty replacement", writer.rows)
	}
}

type fakeCodeReachabilityInputLoader struct {
	inputs []CodeReachabilityProjectionInput
}

func (f *fakeCodeReachabilityInputLoader) LoadPendingCodeReachabilityInputs(
	_ context.Context,
	_ int,
) ([]CodeReachabilityProjectionInput, error) {
	return append([]CodeReachabilityProjectionInput(nil), f.inputs...), nil
}

type fakeCodeReachabilityRowWriter struct {
	scopeID      string
	generationID string
	repositoryID string
	watermark    time.Time
	truncated    bool
	rows         []CodeReachabilityRow
	verdicts     []CodeRootVerdictRow
}

func (f *fakeCodeReachabilityRowWriter) ReplaceRepositoryRows(
	_ context.Context,
	scopeID string,
	generationID string,
	repositoryID string,
	rows []CodeReachabilityRow,
	verdicts []CodeRootVerdictRow,
	watermark time.Time,
	truncated bool,
) error {
	f.scopeID = scopeID
	f.generationID = generationID
	f.repositoryID = repositoryID
	f.watermark = watermark
	f.truncated = truncated
	f.rows = append(f.rows, rows...)
	f.verdicts = append(f.verdicts, verdicts...)
	return nil
}
