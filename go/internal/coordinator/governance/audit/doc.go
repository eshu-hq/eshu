// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package audit shapes the redacted identity carried by the coordinator's
// governance audit events.
//
// Hash and CorrelationID render scope and correlation identities that name a
// decision without disclosing what it was made about. ServiceID is the
// service-principal identity every coordinator-emitted event is attributed
// to, and AppendTimeout bounds one append so a slow audit store cannot stall
// the reconcile or claim path that emitted the event.
//
// The package holds no appender interface: each consumer declares the append
// surface it needs. It imports nothing from the coordinator root, so the root
// and the subpackages that emit audit events can both depend on it.
package audit
