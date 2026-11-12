// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// retireUnclaimedQueryer scripts the two statements the delegation runs:
// the instance gate, then the retire. It records the retire bind args so
// the test proves the (kind, instance, note) triple passes through.
type retireUnclaimedQueryer struct {
	gateKnown  bool
	gateActive bool
	retired    [][2]string
	retireArgs []any
}

func (f *retireUnclaimedQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	// Route the retire statement first: it carries its own deconfiguration
	// recheck against collector_instances (eshu-hq/eshu#10532 F2), so a
	// "collector_instances" substring match alone would misroute it to
	// the gate.
	if strings.Contains(query, "failed_terminal") {
		f.retireArgs = append([]any(nil), args...)
		return &retirePairRows{pairs: f.retired}, nil
	}
	if strings.Contains(query, "FROM collector_instances") {
		return &retireBoolRows{known: f.gateKnown, active: f.gateActive}, nil
	}
	f.retireArgs = append([]any(nil), args...)
	return &retirePairRows{pairs: f.retired}, nil
}

func (f *retireUnclaimedQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, errors.New("unexpected ExecContext call")
}

type retireBoolRows struct {
	known  bool
	active bool
	done   bool
}

func (r *retireBoolRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *retireBoolRows) Scan(dest ...any) error {
	if len(dest) != 2 {
		return errors.New("gate row scans exactly two destinations")
	}
	known, ok := dest[0].(*bool)
	if !ok {
		return errors.New("gate row needs *bool")
	}
	active, ok := dest[1].(*bool)
	if !ok {
		return errors.New("gate row needs *bool")
	}
	*known, *active = r.known, r.active
	return nil
}
func (r *retireBoolRows) Err() error   { return nil }
func (r *retireBoolRows) Close() error { return nil }

type retirePairRows struct {
	pairs [][2]string
	index int
}

func (r *retirePairRows) Next() bool { return r.index < len(r.pairs) }
func (r *retirePairRows) Scan(dest ...any) error {
	if r.index >= len(r.pairs) {
		return errors.New("scan called without row")
	}
	if len(dest) != 2 {
		return errors.New("pair row scans exactly two destinations")
	}
	a, ok := dest[0].(*string)
	if !ok {
		return errors.New("pair row needs *string")
	}
	b, ok := dest[1].(*string)
	if !ok {
		return errors.New("pair row needs *string")
	}
	*a, *b = r.pairs[r.index][0], r.pairs[r.index][1]
	r.index++
	return nil
}
func (r *retirePairRows) Err() error   { return nil }
func (r *retirePairRows) Close() error { return nil }

// TestRetireUnclaimedWorkflowItemsDelegates proves the wrapper calls the
// owning store with the (kind, instance, note) triple and maps the retired
// rows onto the admin port shape.
func TestRetireUnclaimedWorkflowItemsDelegates(t *testing.T) {
	t.Parallel()

	database := &retireUnclaimedQueryer{
		gateKnown: true,
		retired:   [][2]string{{"w-2", "scope-b"}, {"w-1", "scope-a"}},
	}
	s := &postgresStore{database: database}

	items, err := s.RetireUnclaimedWorkflowItems(context.Background(), "sbom_attestation", "sbom-gone", "note")
	if err != nil {
		t.Fatalf("RetireUnclaimedWorkflowItems() error = %v, want nil", err)
	}
	if len(items) != 2 || items[0].WorkItemID != "w-2" || items[1].ScopeID != "scope-a" {
		t.Fatalf("items = %+v, want the delegated rows", items)
	}
	var sawKind, sawInstance, sawNote bool
	for _, arg := range database.retireArgs {
		if s, ok := arg.(string); ok {
			sawKind = sawKind || s == "sbom_attestation"
			sawInstance = sawInstance || s == "sbom-gone"
			sawNote = sawNote || strings.Contains(s, "note")
		}
	}
	if !sawKind || !sawInstance || !sawNote {
		t.Errorf("retire args = %v, want kind, instance, and note", database.retireArgs)
	}
}

// TestRetireUnclaimedWorkflowItemsMapsGateRefusals proves the wrapper maps
// the owning store's gate sentinels onto the admin port's errors, so the
// handler's errors.Is mapping and status codes hold end to end.
func TestRetireUnclaimedWorkflowItemsMapsGateRefusals(t *testing.T) {
	t.Parallel()

	unknown := &postgresStore{database: &retireUnclaimedQueryer{}}
	if _, err := unknown.RetireUnclaimedWorkflowItems(context.Background(), "sbom_attestation", "nope", ""); !errors.Is(err, admin.ErrRetireUnclaimedUnknownInstance) {
		t.Errorf("unknown instance error = %v, want ErrRetireUnclaimedUnknownInstance", err)
	}
	active := &postgresStore{database: &retireUnclaimedQueryer{gateKnown: true, gateActive: true}}
	if _, err := active.RetireUnclaimedWorkflowItems(context.Background(), "sbom_attestation", "live", ""); !errors.Is(err, admin.ErrRetireUnclaimedInstanceActive) {
		t.Errorf("active instance error = %v, want ErrRetireUnclaimedInstanceActive", err)
	}
}

// TestRetireUnclaimedWorkflowItemsEmitsCounter proves the admin path carries
// instruments into the owning store: a successful retire through the admin
// store emits one Sum data point with the call's retired count labeled by
// kind and instance, never a scope id (eshu-hq/eshu#10532 F1).
func TestRetireUnclaimedWorkflowItemsEmitsCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	database := &retireUnclaimedQueryer{
		gateKnown: true,
		retired:   [][2]string{{"w-1", "scope-1"}, {"w-2", "scope-2"}},
	}
	s := (&postgresStore{database: database}).WithInstruments(instruments)

	items, err := s.RetireUnclaimedWorkflowItems(context.Background(), "sbom_attestation", "sbom-gone", "note")
	if err != nil {
		t.Fatalf("RetireUnclaimedWorkflowItems() error = %v, want nil", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2 retired rows", items)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	found := false
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_workflow_unclaimed_retired_total" {
				continue
			}
			found = true
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 2 {
				t.Fatalf("retired metric = %#v, want one data point with value 2", m.Data)
			}
			for _, attr := range sum.DataPoints[0].Attributes.ToSlice() {
				if string(attr.Key) == "scope_id" {
					t.Fatalf("retired labels carry scope_id: scope ids must never be metric labels")
				}
			}
			attrs := map[string]string{}
			for _, attr := range sum.DataPoints[0].Attributes.ToSlice() {
				attrs[string(attr.Key)] = attr.Value.AsString()
			}
			if attrs["collector_kind"] != "sbom_attestation" || attrs["collector_instance_id"] != "sbom-gone" {
				t.Fatalf("retired labels = %v, want the request's kind and instance", attrs)
			}
		}
	}
	if !found {
		t.Fatal("eshu_dp_workflow_unclaimed_retired_total not emitted through the admin store")
	}
}
