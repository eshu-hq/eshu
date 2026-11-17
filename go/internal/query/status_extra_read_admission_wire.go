// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// NewStatusExtraReadAdmission composes the two Postgres readers behind
// the versioned #9669 D6 StatusExtraReadAdmission pin: the bounded
// LiveActivity read and the retirement-summary read. The handler legs
// admit through the pin's embedded ports; the narrow fields stay as the
// construction seam (go/cmd/api builds them from the guarded reader and
// cannot be edited in this lane), and any pair of satisfiers — including
// pin-typed values held by a dest lifter — composes here.
//
// A nil half stays nil inside the composition: each leg nil-guards its
// own embedded port before calling, so a partially wired handler keeps
// the standing per-leg behavior (503 for live activity, omission for
// retirements) instead of panicking on a nil embed.
func NewStatusExtraReadAdmission(live LiveActivityReader, retirements status.IndexRetirementReader) StatusExtraReadAdmission {
	if live == nil && retirements == nil {
		return nil
	}
	return &statusExtraReadAdmission{
		LiveActivityReader:    live,
		IndexRetirementReader: retirements,
	}
}

// NewStatusExtraReadAdmissionWithQueryer builds the D6 extra-port
// admission over the guarded read-only query port, so the satellite
// consumes the pin without a direct table or queue handle: pass the
// request-scoped db.Queryer the same way the API wiring does. It serves
// exactly the two pinned reads over nothing wider than the guarded
// Queryer the satellite holds.
//
// The constructor delegates to the existing Postgres stores deliberately
// instead of minting parallel wiring: the fail-closed proof over a
// guarded Queryer plus the per-leg error-shape parity with the handler's
// narrow ports covers the satellite path end to end.
func NewStatusExtraReadAdmissionWithQueryer(database db.Queryer) StatusExtraReadAdmission {
	if database == nil {
		return nil
	}
	return NewStatusExtraReadAdmission(
		postgres.NewLiveActivityStore(database),
		postgres.NewRetirementStore(database),
	)
}

// statusExtraReadAdmission is the production composition behind the D6
// extra-port pin: the two narrow family-scoped readers, nothing wider.
type statusExtraReadAdmission struct {
	LiveActivityReader
	status.IndexRetirementReader
}

// The composition satisfies the pin with the minimal shape: the two
// embedded ports, no shared graph port, no snapshot reader.
var _ StatusExtraReadAdmission = (*statusExtraReadAdmission)(nil)

// Lifter assignability: the pin's method set covers each handler-facing
// narrow port, so a dest lifter (or a test double) holding a pin-typed
// value wires into the handler's narrow fields with no translation.
var (
	_ LiveActivityReader           = StatusExtraReadAdmission(nil)
	_ status.IndexRetirementReader = StatusExtraReadAdmission(nil)
)

// extraAdmission composes the handler's narrow readers into the D6 pin
// for the legs below. A nil handler yields nil; nil halves stay nil and
// each leg guards its own port — the same fail-closed shape as the
// direct narrow reads this replaces.
func (h *StatusHandler) extraAdmission() *statusExtraReadAdmission {
	if h == nil {
		return nil
	}
	return &statusExtraReadAdmission{
		LiveActivityReader:    h.LiveActivity,
		IndexRetirementReader: h.Retirements,
	}
}
