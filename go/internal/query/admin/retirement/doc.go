// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retirement holds the operator-driven repository retirement
// trigger (issue #8602): the admin POST /api/v0/admin/repository-retirements
// route and the Trigger that runs mark, fence, and purge in the designed
// order. The tombstone store, the phase-1 fence, and the narrowed purge live
// in storage/postgres (#7766 foundation); this package owns only the operator
// sequencing, the outcome telemetry, and the audit events, so the trigger is
// the single production caller of RetirementFencer and RetirementPurger.
package retirement
