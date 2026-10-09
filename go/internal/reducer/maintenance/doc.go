// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package maintenance is the parent index of the reducer's periodic
// side-runner leaves. Each family lives in its own leaf package with
// destuttered names (issue #7648); this package holds no runner logic
// itself.
//
// The leaves are accepted (repo-dependency activation-gate decorators),
// obligation (activation obligation consumer, #7584), evidence
// (collector-readiness evidence summary resweep, #3466), liveness
// (generation liveness recovery), retention (generation retention
// pruning), orphan (graph orphan sweep), infra (infra read model
// reconcile, #6793), poison (poison dead-letter liveness, #4740),
// producer (producer activation consumer, #7635), and testutil (shared
// metric-reading test helpers). Each one runs beside the main
// claim/execute/ack loop as a [reducer.Service] side-runner; none owns
// intent claiming or graph projection itself.
//
// Every leaf may import reducer/sharedintent, internal/telemetry, and
// pkg/log, and must never import the parent internal/reducer package,
// directly or transitively (issue #6061). Contracts the reducer root
// owns are mirrored locally in the leaf that needs them; see each
// leaf's doc.go.
package maintenance
