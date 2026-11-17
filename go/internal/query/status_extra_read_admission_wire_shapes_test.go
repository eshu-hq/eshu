// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errStatusExtraWireShapeProbe is the sentinel the failing guarded store
// below answers every read with. A non-nil error (never a panic, never a
// confident empty result) is the fail-closed proof the
// satellite-consumable constructor wires the guarded port through.
var errStatusExtraWireShapeProbe = errors.New("status extra wire-shape probe: store unavailable")

// errStatusExtraWireShapeQueryer is a guarded store that answers every
// read with the probe error. It implements the db.Queryer surface the
// Postgres readers need, so constructing the admission satisfier over it
// proves the satellite path needs nothing wider than the request-scoped
// Queryer handle.
type errStatusExtraWireShapeQueryer struct{ err error }

func (s errStatusExtraWireShapeQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

var _ db.Queryer = errStatusExtraWireShapeQueryer{}

// TestStatusExtraReadAdmissionWireShapesAreSatelliteConsumable proves the
// wire shape behind the #9669 D6 StatusExtraReadAdmission pin: the
// WithQueryer constructor serves both pinned reads over nothing wider
// than the guarded Queryer the satellite holds. Each read fails closed
// (never panics, never returns a confident empty result with a nil
// error) when the guarded store is unavailable.
func TestStatusExtraReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	admission := NewStatusExtraReadAdmissionWithQueryer(errStatusExtraWireShapeQueryer{err: errStatusExtraWireShapeProbe})
	if admission == nil {
		t.Fatal("NewStatusExtraReadAdmissionWithQueryer() = nil, want a StatusExtraReadAdmission bound to the guarded store")
	}

	if _, _, err := admission.ReadLiveActivity(context.Background(), 10, true, nil, nil); err == nil {
		t.Error("ReadLiveActivity over an unavailable store = nil error, want a fail-closed error")
	}
	if _, err := admission.ReadRetirementSummary(context.Background()); err == nil {
		t.Error("ReadRetirementSummary over an unavailable store = nil error, want a fail-closed error")
	}
}

// TestStatusExtraReadAdmissionMirrorStaysHonest proves the production
// composition behind the pin answers the same transports the handler
// legs admit through: the composed admission satisfies both the
// Status-scoped pin and each handler-facing narrow port, so satellite
// consumption moves no read off the shape the routes already admit
// through.
func TestStatusExtraReadAdmissionMirrorStaysHonest(t *testing.T) {
	t.Parallel()

	admission := NewStatusExtraReadAdmissionWithQueryer(errStatusExtraWireShapeQueryer{err: errStatusExtraWireShapeProbe})
	var _ LiveActivityReader = admission
	var _ status.IndexRetirementReader = admission
}

// TestStatusExtraReadAdmissionComposesPartialWiring proves the handler
// composer keeps the standing per-leg fail-closed shape: all-nil
// readers compose to nil, and a half-wired composition still exposes
// the missing embed as nil so each leg guards its own port instead of
// panicking on a nil embed.
func TestStatusExtraReadAdmissionComposesPartialWiring(t *testing.T) {
	t.Parallel()

	if NewStatusExtraReadAdmission(nil, nil) != nil {
		t.Error("NewStatusExtraReadAdmission(nil, nil) = non-nil, want nil: an unwired pair must fail closed")
	}
	if NewStatusExtraReadAdmissionWithQueryer(nil) != nil {
		t.Error("NewStatusExtraReadAdmissionWithQueryer(nil) = non-nil, want nil: an unwired store must fail closed")
	}

	full := NewStatusExtraReadAdmissionWithQueryer(errStatusExtraWireShapeQueryer{err: errStatusExtraWireShapeProbe})
	half := NewStatusExtraReadAdmission(full, nil)
	if half == nil {
		t.Fatal("half-wired composition = nil, want a pin carrying the live half")
	}

	// Struct-level: the legs guard the embeds, so prove the composer
	// preserves nil halves instead of hiding them.
	bare := (&StatusHandler{}).extraAdmission()
	if bare == nil {
		t.Fatal("extraAdmission on a bare handler = nil, want a composition carrying nil embeds")
	}
	if bare.LiveActivityReader != nil || bare.IndexRetirementReader != nil {
		t.Error("extraAdmission on a bare handler hid a nil embed: legs would skip their fail-closed guards")
	}
	partial := (&StatusHandler{LiveActivity: full}).extraAdmission()
	if partial.LiveActivityReader == nil {
		t.Error("extraAdmission dropped the wired LiveActivity half")
	}
	if partial.IndexRetirementReader != nil {
		t.Error("extraAdmission invented an IndexRetirementReader half: the retirements leg would stop omitting")
	}
}
