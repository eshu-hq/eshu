// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// GovernanceAuditAdmission is the admission boundary for governance-audit
// append: it aliases GovernanceAuditAppender so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff for
// this family. The seam is append-only on purpose: the aliased interface
// carries the single Append method and no read surface (no Summary, no
// List), so a dest consumer implementing only this contract can record
// audit events but cannot acquire audit readback through it.
type GovernanceAuditAdmission = GovernanceAuditAppender

// GovernanceAuditAdmissionContractVersion pins the versioned shape of
// GovernanceAuditAdmission. Any breaking change to the method set must bump
// this alongside the frozen-list update in
// governance_audit_admission_contract_test.go so dest consumers can pin the
// shape.
const GovernanceAuditAdmissionContractVersion = "v0"

// GovernanceAuditDropObserver observes one swallowed audit-append failure:
// an Append that returned err after the caller already decided the primary
// operation survives it (the query request-path and bootstrap-seed call
// sites best-effort with `_ =`, per go/cmd/api/seed_initial_admin_audit.go
// and go/internal/query/auth_audit.go; coordinator egress paths propagate
// the error instead). err is the sink error, unchanged;
// eventCount is the number of events that did not persist. The observer must
// be cheap and non-blocking: it runs on the request path that just failed to
// persist audit data. A satellite-native runtime wires its dropped-audit
// counter here; in-tree call sites pass nil until that port exists.
type GovernanceAuditDropObserver func(ctx context.Context, err error, eventCount int)

// ObservedGovernanceAuditAppender wraps a GovernanceAuditAppender with an
// opt-in drop observer. It still satisfies GovernanceAuditAdmission (the
// frozen single-method set), so wrapping a sink never leaves the versioned
// contract. On success it forwards the events untouched and stays silent; on
// failure it reports the drop through OnDrop and returns the sink error
// unchanged, preserving each caller's own policy: request-path and seed
// call sites best-effort past the error while coordinator egress paths
// propagate it. A nil OnDrop is a
// safe no-op, and the zero value (nil Inner, e.g. audit store unavailable)
// is a silent skip returning nil, matching the existing nil-appender
// convention at every call site.
type ObservedGovernanceAuditAppender struct {
	// Inner is the durable sink events forward to. Nil means unavailable:
	// Append skips silently and returns nil.
	Inner GovernanceAuditAppender
	// OnDrop fires once per failed Append with the sink error and the
	// dropped event count. Nil disables observation.
	OnDrop GovernanceAuditDropObserver
}

// Compile-time proof the wrapper stays on the versioned seam.
var _ GovernanceAuditAdmission = ObservedGovernanceAuditAppender{}

// Append forwards events to the inner sink, reporting a failed append
// through OnDrop while returning the sink error unchanged.
func (o ObservedGovernanceAuditAppender) Append(ctx context.Context, events []governanceaudit.Event) error {
	if o.Inner == nil {
		return nil
	}
	err := o.Inner.Append(ctx, events)
	if err != nil && o.OnDrop != nil {
		o.OnDrop(ctx, err, len(events))
	}
	return err
}
