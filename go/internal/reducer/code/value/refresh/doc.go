// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package refresh re-runs the global value-flow fixpoint after late producers
// land (issue #6785). [Handler] executes one code_value_flow_refresh intent by
// calling only the fixpoint projector: summaries, sources, and graph ids are
// unchanged by the producers whose completion enqueues the refresh, and the
// fixpoint reloads them globally before solving.
//
// The refresh item is a singleton anchored to the migration-seeded eshu:global
// scope, so producer completions in any later generation reopen the same row
// through the existing durable completion fanout. It serializes against
// itself on a global conflict key; overlap with concurrent per-repo summary
// fixpoint runs is the pre-existing race class the #6785 design note files
// separately.
package refresh
