// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	"github.com/eshu-hq/eshu/go/internal/status/generation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errFreshnessWireShapeProbe is the sentinel the failing guarded
// reader below answers every read with. A non-nil error (never a
// panic, never a confident empty result) is the fail-closed proof the
// satellite-consumable constructor wires the guarded port through.
var errFreshnessWireShapeProbe = errors.New("freshness wire-shape probe: reader unavailable")

// errWireShapeRow is a db.Row that always fails its scan.
type errWireShapeRow struct{ err error }

func (r errWireShapeRow) Scan(...any) error { return r.err }

// errWireShapeReadStore is a guarded reader that answers every read
// with the probe error. It implements the full db.ReadStore surface
// (cursor, row, and snapshot reads), so constructing the admission
// satisfier over it proves the satellite path needs nothing wider
// than the request-scoped ReadStore handle.
type errWireShapeReadStore struct{ err error }

func (s errWireShapeReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func (s errWireShapeReadStore) QueryRowContext(_ context.Context, _ string, _ ...any) db.Row {
	return errWireShapeRow(s)
}

func (s errWireShapeReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, s.err
}

var _ db.ReadStore = errWireShapeReadStore{}

// TestFreshnessReadAdmissionWireShapesAreSatelliteConsumable proves
// the wire shapes behind the #9669 D13 Wave-1 pin: the production
// Postgres status store satisfies the admission, and the
// WithReadStore constructor serves all three re-declared reads over
// nothing wider than the guarded ReadStore the satellite holds. Every
// read fails closed (never panics, never returns confident emptiness)
// when the guarded reader is unavailable.
func TestFreshnessReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	var _ FreshnessReadAdmission = postgres.NewStatusStore(errWireShapeReadStore{err: errFreshnessWireShapeProbe})

	admission := NewReadAdmissionWithReadStore(errWireShapeReadStore{err: errFreshnessWireShapeProbe})
	if admission == nil {
		t.Fatal("NewReadAdmissionWithReadStore() = nil, want a FreshnessReadAdmission bound to the guarded reader")
	}

	ctx := context.Background()
	if _, err := admission.ComputeChangedSinceDelta(ctx, changedsince.Filter{}); err == nil {
		t.Error("ComputeChangedSinceDelta over an unavailable reader = nil error, want a fail-closed error")
	}
	if _, err := admission.ComputeServiceChangedSinceDelta(ctx, changedsince.ServiceFilter{}); err == nil {
		t.Error("ComputeServiceChangedSinceDelta over an unavailable reader = nil error, want a fail-closed error")
	}
	if _, err := admission.ListGenerationLifecycle(ctx, generation.LifecycleFilter{}); err == nil {
		t.Error("ListGenerationLifecycle over an unavailable reader = nil error, want a fail-closed error")
	}
}

// TestFreshnessReadAdmissionWireShapesAreSealed proves the Wave-1
// seal on the pin: exactly the three re-declared freshness reads,
// with no ledger, stored-summary, or snapshot surface riding along.
// The frozen method-set contract test pins the exact names; this
// test pins the exclusion the arbiter ruling demands.
func TestFreshnessReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*FreshnessReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("FreshnessReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Snapshot", "Ledger", "Summary", "StartupError"} {
			if strings.Contains(name, banned) {
				t.Errorf("FreshnessReadAdmission.%s contains %q: ledger, stored-summary, and snapshot legs stay out of this pin", name, banned)
			}
		}
	}
}
