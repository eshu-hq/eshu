// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var deltaUnionTestObserved = time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)

func deltaUnionFactRow(factID, generation, kind, key string, payload string) []any {
	return []any{
		factID,
		"scope-1",
		generation,
		kind,
		key,
		"1.0.0",
		"git",
		int64(0),
		"observed",
		"git",
		"fact-key",
		"file:///repo/path",
		"record-1",
		deltaUnionTestObserved,
		false,
		[]byte(payload),
	}
}

func deltaUnionDeltaRows() [][]any {
	return [][]any{
		deltaUnionFactRow("delta-repo", "gen-delta-1", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha","delta_generation":true,"delta_relative_paths":["keep.go"],"delta_deleted_relative_paths":["w.go"]}`),
		deltaUnionFactRow("delta-keep", "gen-delta-1", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`),
	}
}

func deltaUnionBaseRows() [][]any {
	return [][]any{
		deltaUnionFactRow("base-repo", "gen-base-1", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha"}`),
		deltaUnionFactRow("base-keep", "gen-base-1", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`),
		deltaUnionFactRow("base-w", "gen-base-1", "file", "file:repo-1:w.go",
			`{"repo_id":"repo-1","relative_path":"w.go"}`),
		deltaUnionFactRow("base-other", "gen-base-1", "file", "file:repo-1:other.go",
			`{"repo_id":"repo-1","relative_path":"other.go"}`),
	}
}

func deltaUnionFactIDs(t *testing.T, factKinds map[string]string) []string {
	t.Helper()
	ids := make([]string, 0, len(factKinds))
	for id := range factKinds {
		ids = append(ids, id)
	}
	return ids
}

func TestFactStoreListFactsFullGenerationSkipsUnion(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{false, "", ""}}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-full-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("ListFacts() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d (facts plus target row, no base lookup)", got, want)
	}
}

func TestFactStoreListFactsDeltaOverlaysBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	byID := make(map[string]string, len(loaded))
	for _, env := range loaded {
		byID[env.FactID] = env.GenerationID
	}
	// Delta rows win: the delta repo fact replaces the base repo fact and
	// the changed file replaces its base row; the deleted file's base row
	// is gone while the untouched base row survives.
	for _, id := range []string{"delta-repo", "delta-keep", "base-other"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("union is missing fact %q: %v", id, deltaUnionFactIDs(t, byID))
		}
	}
	for _, absent := range []string{"base-repo", "base-keep", "base-w"} {
		if _, ok := byID[absent]; ok {
			t.Fatalf("union contains dropped base fact %q", absent)
		}
	}
	// Base rows keep their own generation identity.
	if got := byID["base-other"]; got != "gen-base-1" {
		t.Fatalf("base fact generation = %q, want gen-base-1", got)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[1].query, "delta_baseline_commit_sha") {
		t.Fatalf("second query = %q, want generation target read", db.queries[1].query)
	}
	if !strings.Contains(db.queries[2].query, "activated_at IS NOT NULL") {
		t.Fatalf("third query = %q, want projected-base lookup", db.queries[2].query)
	}
}

func TestFactStoreListFactsDeltaBaseMissingRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-pruned", ""}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	if missing.ScopeID != "scope-1" || missing.GenerationID != "gen-delta-1" || missing.BaselineCommitSHA != "commit-pruned" {
		t.Fatalf("refusal = %+v, want scope/generation/baseline", missing)
	}
}

func TestFactStoreListFactsLegacyDeltaLoadsSingleGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "", ""}}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("ListFacts() len = %d, want %d (no union without a baseline)", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestFactStoreCountFactsDeltaReturnsUnionBound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: [][]any{{90}}},
			{rows: [][]any{{5}}},
		},
	}
	store := NewFactStore(db)

	count, err := store.CountFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("CountFacts() error = %v, want nil", err)
	}
	if got, want := count, 95; got != want {
		t.Fatalf("CountFacts() = %d, want %d (base plus delta)", got, want)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestFactStoreCountFactsBaseMissingRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true, "commit-pruned", ""}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	_, err := store.CountFacts(context.Background(), "scope-1", "gen-delta-1")
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("CountFacts() error = %v, want DeltaBaseMissingError", err)
	}
}

// The ACTIVE Entity flag must count only the entity types the graph readers
// count (#7797 P1): both graph readers count (f)-[:CONTAINS]->(e:Function)
// nodes, while content_entity rows also carry Module/Parameter/Variable
// types the projection never writes as :Function nodes under the file.
// Flagging every content_entity row reads correct graphs as Drifted.
func TestDeltaUnionActiveFactsQueryCountsOnlyFunctionEntities(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"entity_type",
		"IN ('function', 'Function')",
	} {
		if !strings.Contains(deltaUnionActiveFactsQuery, fragment) {
			t.Fatalf("deltaUnionActiveFactsQuery is missing %q", fragment)
		}
	}
}

// The base-lookup predicates must keep the status-led bitmap plan (no
// dedicated index dependency): a predicate edit that drops the planner off
// the bitmap is a silent per-projection rescan of the scope's generation
// history. Reintroducing a dedicated base index is tracked in #8198; until
// then these predicates stay bitmap-bound and must not be read as index
// predicates.
func TestDeltaUnionBaseQueryKeepsPartialIndexPredicates(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"scope_id = $1",
		"is_delta = false",
		"activated_at IS NOT NULL",
		"status IN ('active', 'completed', 'superseded')",
		"source_commit_sha = $2",
		"ORDER BY (status IN ('active', 'completed')) DESC, ingested_at DESC, generation_id DESC",
	} {
		if !strings.Contains(deltaUnionBaseQuery, fragment) {
			t.Fatalf("deltaUnionBaseQuery is missing %q", fragment)
		}
	}
}

// An active base skips the retention probe: active generations are never
// prune-eligible, so the union costs no extra query for the common case.
func TestFactStoreListFactsActiveBaseSkipsPruneProbe(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db)

	if _, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1"); err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (no prune probe for an active base)", got, want)
	}
}

// A superseded base with no retention event overlays normally after one
// indexed probe.
func TestFactStoreListFactsSupersededBaseWithoutPruneEventOverlays(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "superseded"}}},
			{rows: nil},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 3; got != want {
		t.Fatalf("ListFacts() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 5; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[3].query, "generation_retention_events") {
		t.Fatalf("fourth query = %q, want the retention probe", db.queries[3].query)
	}
}

// A superseded base with a retention prune event refuses: retention may
// have taken part of its facts, so overlaying would restore only a truncated
// tree.
func TestFactStoreListFactsPrunedSupersededBaseRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "superseded"}}},
			{rows: [][]any{{1}}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (no base facts read after refusal)", got, want)
	}
}

// A failing retention probe on a superseded base refuses (#8181): the probe
// cannot confirm the base survived retention intact, and the surviving base
// rows below model a half-pruned base, so overlaying them would project a
// complete-looking tree from a partial base. The read fails closed with the
// existing refused outcome instead of projecting the survivors.
func TestFactStoreListFactsProbeErrorOnSupersededBaseRefuses(t *testing.T) {
	t.Parallel()

	inst, reader := newDeltaUnionTestInstruments(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "superseded"}}},
			{rows: nil, err: errors.New("retention probe outage")},
			{rows: deltaUnionBaseRows()},
		},
	}
	var logs bytes.Buffer
	store := NewFactStore(db).WithInstruments(inst).WithLogger(
		slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal (no partial projection)", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	if missing.ScopeID != "scope-1" || missing.GenerationID != "gen-delta-1" || missing.BaselineCommitSHA != "commit-base" {
		t.Fatalf("refusal = %+v, want scope/generation/baseline", missing)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (no base facts read after refusal)", got, want)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionBaseMissingRefused)); got != 1 {
		t.Fatalf("union_base_missing_refused = %d, want 1 (existing outcome label)", got)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionResolveFailed)); got != 0 {
		t.Fatalf("union_resolve_failed = %d, want 0 (a probe outage is a refusal, not a retry)", got)
	}
	if logged := logs.String(); !strings.Contains(logged, "delta union retention probe failed; refusing closed") ||
		!strings.Contains(logged, "retention probe outage") {
		t.Fatalf("refusal log = %q, want the probe-failure debug line carrying the cause", logged)
	}
}

// newDeltaUnionTestInstruments builds instruments over a manual reader so
// union outcome tests can assert the exact counted outcome.
func newDeltaUnionTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// deltaUnionOutcomeValue reads one outcome's count from
// eshu_dp_projector_delta_union_total, 0 when the outcome was never counted.
func deltaUnionOutcomeValue(t *testing.T, rm metricdata.ResourceMetrics, outcome string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_projector_delta_union_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("delta union counter data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				if value, ok := point.Attributes.Value("outcome"); ok && value.AsString() == outcome {
					return point.Value
				}
			}
		}
	}
	return 0
}

// A resolve error that is not a missing base must not record the refused
// outcome (#7797 P2): only *projector.DeltaBaseMissingError takes the
// refusal path in processWork; any other error retries through the generic
// fail path, so the signal must stay distinct.
func TestFactStoreListFactsResolveErrorRecordsResolveFailedOutcome(t *testing.T) {
	t.Parallel()

	inst, reader := newDeltaUnionTestInstruments(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: nil, err: errors.New("connection refused")},
		},
	}
	store := NewFactStore(db).WithInstruments(inst)

	_, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err == nil {
		t.Fatal("ListFacts() error = nil, want resolve failure")
	}
	var missing *projector.DeltaBaseMissingError
	if errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want a non-refusal resolve error", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionResolveFailed)); got != 1 {
		t.Fatalf("union_resolve_failed = %d, want 1", got)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionBaseMissingRefused)); got != 0 {
		t.Fatalf("union_base_missing_refused = %d, want 0 (a retryable resolve error is not a refusal)", got)
	}
}

// A missing base still records the refused outcome: the errors.As
// distinction must not swallow the genuine refusal signal.
func TestFactStoreListFactsBaseMissingRecordsRefusedOutcome(t *testing.T) {
	t.Parallel()

	inst, reader := newDeltaUnionTestInstruments(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-pruned", ""}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db).WithInstruments(inst)

	_, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionBaseMissingRefused)); got != 1 {
		t.Fatalf("union_base_missing_refused = %d, want 1", got)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionResolveFailed)); got != 0 {
		t.Fatalf("union_resolve_failed = %d, want 0 (a genuine refusal is not a resolve failure)", got)
	}
}

// deltaUnionNonDeltaScopeRows models a delta whose repository fact carries
// no delta_generation flag: the DB row says delta with a baseline but the
// payload parses to a non-delta scope.
func deltaUnionNonDeltaScopeRows() [][]any {
	return [][]any{
		deltaUnionFactRow("delta-repo", "gen-delta-1", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha"}`),
		deltaUnionFactRow("delta-keep", "gen-delta-1", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`),
	}
}

// A resolved union whose delta carries no delta_generation repository fact
// must WARN and count the scope-mismatch outcome (#7797 P2): without the
// delta scope the branch returns base+delta concatenation with no dedup,
// and the DB-versus-payload divergence needs a signal.
func TestFactStoreListFactsNonDeltaScopeWarnsAndCountsMismatch(t *testing.T) {
	t.Parallel()

	inst, reader := newDeltaUnionTestInstruments(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionNonDeltaScopeRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db).WithInstruments(inst).WithLogger(logger)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil (the load proceeds additively)", err)
	}
	// No dedup without a delta scope: every delta and base row survives.
	if got, want := len(loaded), 2+len(deltaUnionBaseRows()); got != want {
		t.Fatalf("ListFacts() len = %d, want %d (additive concatenation)", got, want)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := deltaUnionOutcomeValue(t, rm, string(projector.DeltaUnionScopeMismatch)); got != 1 {
		t.Fatalf("union_scope_mismatch = %d, want 1", got)
	}
	if got := logs.String(); !strings.Contains(got, "WARN") || !strings.Contains(got, "scope-1") {
		t.Fatalf("logs = %q, want a WARN naming the scope", got)
	}
}

// A base row with no facts refuses: a stored full generation always carries
// at least its repository fact, so an empty read is a drained base.
func TestFactStoreListFactsEmptyBaseRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
}
