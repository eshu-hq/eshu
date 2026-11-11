// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestRetireUnclaimedWorkItemsRefusesUnknownInstance fails closed when no
// collector_instances row exists for the (kind, instance) pair: retiring
// for a never-existing instance is a typo, and the sanctioned path is valid
// only once the instance is deconfigured (eshu-hq/eshu#10532).
func TestRetireUnclaimedWorkItemsRefusesUnknownInstance(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{false, false}}}},
	}
	store := NewWorkflowControlStore(database)

	_, err := store.RetireUnclaimedWorkItems(context.Background(), scope.CollectorSBOMAttestation, "sbom-gone", "operator note")
	if !errors.Is(err, ErrWorkflowRetireUnknownInstance) {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want ErrWorkflowRetireUnknownInstance", err)
	}
	if len(database.execs) != 0 {
		t.Fatalf("refused retire issued %d writes, want 0", len(database.execs))
	}
}

// TestRetireUnclaimedWorkItemsRefusesActiveInstance fails closed while the
// instance row is still enabled and not deactivated: a live collector may
// still claim those pending rows, so the disposition is not valid yet
// (eshu-hq/eshu#10532).
func TestRetireUnclaimedWorkItemsRefusesActiveInstance(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{true, true}}}},
	}
	store := NewWorkflowControlStore(database)

	_, err := store.RetireUnclaimedWorkItems(context.Background(), scope.CollectorSBOMAttestation, "sbom-live", "operator note")
	if !errors.Is(err, ErrWorkflowRetireInstanceActive) {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want ErrWorkflowRetireInstanceActive", err)
	}
	if len(database.execs) != 0 {
		t.Fatalf("refused retire issued %d writes, want 0", len(database.execs))
	}
}

// TestRetireUnclaimedWorkItemsRequiresIdentity rejects empty kind or
// instance before touching the database.
func TestRetireUnclaimedWorkItemsRequiresIdentity(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{}
	store := NewWorkflowControlStore(database)

	if _, err := store.RetireUnclaimedWorkItems(context.Background(), "", "sbom-gone", "note"); err == nil {
		t.Fatal("RetireUnclaimedWorkItems() with empty kind error = nil, want non-nil")
	}
	if _, err := store.RetireUnclaimedWorkItems(context.Background(), scope.CollectorSBOMAttestation, "  ", "note"); err == nil {
		t.Fatal("RetireUnclaimedWorkItems() with blank instance error = nil, want non-nil")
	}
	if len(database.queries) != 0 {
		t.Fatalf("identity refusal issued %d reads, want 0", len(database.queries))
	}
}

// TestRetireUnclaimedWorkItemsLocksSelectedRowsInWorkItemOrder pins the
// lock discipline the claim-versus-retire race depends on: the retire
// selects candidates newest first, locks the still-eligible rows in
// work_item_id COLLATE "C" order under SKIP LOCKED (the order the
// collector claim path locks in, so the two can never deadlock), and
// rechecks every predicate on the locked row before the terminal update
// (eshu-hq/eshu#10532).
func TestRetireUnclaimedWorkItemsLocksSelectedRowsInWorkItemOrder(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true, false}}},
			{rows: [][]any{{"w-2", "scope-b"}, {"w-1", "scope-a"}}},
		},
	}
	store := NewWorkflowControlStore(database)

	retired, err := store.RetireUnclaimedWorkItems(context.Background(), scope.CollectorSBOMAttestation, "sbom-gone", "operator note")
	if err != nil {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if len(retired) != 2 || retired[0].WorkItemID != "w-2" || retired[1].WorkItemID != "w-1" {
		t.Fatalf("retired = %+v, want [w-2 scope-b, w-1 scope-a]", retired)
	}

	var retireSQL string
	var retireArgs []any
	for _, q := range database.queries {
		if strings.Contains(q.query, "failed_terminal") {
			retireSQL = q.query
			retireArgs = q.args
		}
	}
	if retireSQL == "" {
		t.Fatal("retire issued no failed_terminal statement")
	}
	for _, want := range []string{
		`status = 'pending'`,
		`attempt_count = 0`,
		`current_claim_id IS NULL`,
		`ORDER BY work.work_item_id COLLATE "C"`,
		`FOR NO KEY UPDATE`,
		`SKIP LOCKED`,
		`failed_terminal`,
		// The in-statement deconfiguration recheck (eshu-hq/eshu#10532
		// F2): a reactivation that commits between the gate probe and
		// this statement must retire nothing.
		`NOT EXISTS`,
		`collector_instances`,
		`deactivated_at IS NULL`,
	} {
		if !strings.Contains(retireSQL, want) {
			t.Errorf("retire SQL missing %q", want)
		}
	}
	// The failure class and operation ride as bound parameters from the
	// workflow consts, never string-interpolated, so assert the args.
	assertRetireArg(t, retireArgs, string(scope.CollectorSBOMAttestation))
	assertRetireArg(t, retireArgs, "sbom-gone")
	assertRetireArg(t, retireArgs, workflow.FailureClassOperatorRetiredUnclaimed)
	assertRetireArg(t, retireArgs, workflow.FailureOperationOperatorRetire)
}

func assertRetireArg(t *testing.T, args []any, want string) {
	t.Helper()
	for _, arg := range args {
		if s, ok := arg.(string); ok && s == want {
			return
		}
	}
	t.Errorf("retire statement args missing %q; got %v", want, args)
}

// TestRetireUnclaimedWorkItemsEmptyIsIdempotent proves the second call (or
// a call with nothing eligible) retires nothing and reports zero: the
// mutation is state-idempotent like the admin skip (eshu-hq/eshu#10532).
func TestRetireUnclaimedWorkItemsEmptyIsIdempotent(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true, false}}},
			{rows: nil},
		},
	}
	store := NewWorkflowControlStore(database)

	retired, err := store.RetireUnclaimedWorkItems(context.Background(), scope.CollectorSBOMAttestation, "sbom-gone", "note")
	if err != nil {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if len(retired) != 0 {
		t.Fatalf("retired = %+v, want empty", retired)
	}
}

// TestTruncateRetireUnclaimedNoteKeepsRunesIntact proves the operator note
// clamp never splits a multi-byte rune: a 600-rune note of 2-byte runes
// clamps to exactly 500 runes of valid UTF-8, not 500 bytes of mojibake
// (eshu-hq/eshu#10532 F5).
func TestTruncateRetireUnclaimedNoteKeepsRunesIntact(t *testing.T) {
	t.Parallel()

	note := strings.Repeat("é", 600)
	got := truncateRetireUnclaimedNote(note)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated note is not valid UTF-8: %q", got[len(got)-8:])
	}
	if n := utf8.RuneCountInString(got); n != retireUnclaimedOperatorNoteLimit {
		t.Fatalf("truncated note holds %d runes, want %d", n, retireUnclaimedOperatorNoteLimit)
	}
}

// TestRecordUnclaimedRetiredSkipsCounterOnZeroCount proves a zero-count
// retirement logs without touching the counter: a Sum data point with value
// 0 would claim work moved that never moved (eshu-hq/eshu#10532 F6).
func TestRecordUnclaimedRetiredSkipsCounterOnZeroCount(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store := NewWorkflowControlStore(&fakeExecQueryer{})
	store.Instruments = instruments

	store.recordUnclaimedRetired(context.Background(), "sbom_attestation", "sbom-gone", 0)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "eshu_dp_workflow_unclaimed_retired_total" {
				t.Fatalf("zero-count retire emitted %s; want no data point", m.Name)
			}
		}
	}
}

// TestRecordUnclaimedRetiredEmitsCountAndLabels proves the positive path the
// zero-count skip must not break: a count of 2 emits one Sum data point with
// value 2 labeled by kind and instance, never a scope id
// (eshu-hq/eshu#10532 F1/F6).
func TestRecordUnclaimedRetiredEmitsCountAndLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store := NewWorkflowControlStore(&fakeExecQueryer{})
	store.Instruments = instruments

	store.recordUnclaimedRetired(context.Background(), "sbom_attestation", "sbom-gone", 2)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var sum metricdata.Sum[int64]
	found := false
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_workflow_unclaimed_retired_total" {
				continue
			}
			found = true
			var ok bool
			sum, ok = m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("retired metric data = %T, want Sum[int64]", m.Data)
			}
		}
	}
	if !found {
		t.Fatal("eshu_dp_workflow_unclaimed_retired_total not emitted for count 2; saw nothing")
	}
	if len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 2 {
		t.Fatalf("retired sum = %+v, want one data point with value 2", sum.DataPoints)
	}
	attrs := map[string]string{}
	for _, attr := range sum.DataPoints[0].Attributes.ToSlice() {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	if attrs["collector_kind"] != "sbom_attestation" || attrs["collector_instance_id"] != "sbom-gone" {
		t.Fatalf("retired labels = %v, want collector_kind and collector_instance_id", attrs)
	}
	if _, ok := attrs["scope_id"]; ok {
		t.Fatalf("retired labels carry scope_id: scope ids must never be metric labels")
	}
}
