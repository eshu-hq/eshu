// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errStatusGovernanceWireShapeProbe is the sentinel the failing guarded
// store below answers the Summary read with. A non-nil error (never a panic,
// never a confident empty result) is the fail-closed proof the
// satellite-consumable constructor wires the guarded port through.
var errStatusGovernanceWireShapeProbe = errors.New("status governance wire-shape probe: store unavailable")

// errStatusGovernanceWireShapeQueryer is a guarded store that answers every
// read with the probe error. It implements the db.Queryer surface the audit
// reader needs, so constructing the admission satisfier over it proves the
// satellite path needs nothing wider than the request-scoped Queryer handle.
type errStatusGovernanceWireShapeQueryer struct{ err error }

func (s errStatusGovernanceWireShapeQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

var _ db.Queryer = errStatusGovernanceWireShapeQueryer{}

// TestStatusGovernanceReadAdmissionWireShapesAreSatelliteConsumable proves
// the wire shape behind the #9669 Status governance pin: the production
// Postgres audit reader satisfies the admission, and the WithQueryer
// constructor serves the re-declared Summary read over nothing wider than
// the guarded Queryer the satellite holds. The read fails closed (never
// panics, never returns a confident empty summary with a nil error) when the
// guarded store is unavailable.
func TestStatusGovernanceReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	_ = NewStatusGovernanceReadAdmissionWithQueryer(errStatusGovernanceWireShapeQueryer{err: errStatusGovernanceWireShapeProbe})

	admission := NewStatusGovernanceReadAdmissionWithQueryer(errStatusGovernanceWireShapeQueryer{err: errStatusGovernanceWireShapeProbe})
	if admission == nil {
		t.Fatal("NewStatusGovernanceReadAdmissionWithQueryer() = nil, want a StatusGovernanceReadAdmission bound to the guarded store")
	}

	if _, err := admission.Summary(context.Background()); err == nil {
		t.Error("Summary over an unavailable store = nil error, want a fail-closed error")
	}
}

// TestStatusGovernanceReadAdmissionMirrorStaysHonest proves the production
// audit reader behind the pin answers the same transport the handler port
// does: the auditstore reader value the constructor delegates to satisfies
// both the Status-scoped pin and the handler-facing
// GovernanceAuditSummaryReader port, so satellite consumption moves no read
// off the shape the route already admits through.
func TestStatusGovernanceReadAdmissionMirrorStaysHonest(t *testing.T) {
	t.Parallel()

	admission := NewStatusGovernanceReadAdmissionWithQueryer(errStatusGovernanceWireShapeQueryer{err: errStatusGovernanceWireShapeProbe})
	var _ GovernanceAuditSummaryReader = admission
}
