// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retire runs the operator-driven retirement of the name-only Workload
// nodes the repository-scoped identity change (#5385) left behind (#8795).
//
// [Runner.Run] serves POST /admin/retire-legacy-workloads on the reducer admin
// listener. A dry run reads only: it asks the shared detection in
// graph/legacy/workload which leftovers are eligible and why the rest are not.
// An execute reads the same way, then sends one auto-commit statement over at
// most [MaxLimit] eligible ids. That statement takes each node's lock and then
// re-evaluates every retirement clause, so the dry run is advisory and the
// delete is the gate. Both are offered on Neo4j only, the one backend they
// were tested on; on any other backend the runner refuses both with
// legacyworkload.ErrBackendUnvalidated. The request, response, and error types
// live in graph/legacy/workload so the runtime admin route need not import
// this package.
//
// Retirement is never automatic. The reducer does not call this package from
// any queue path, because the rollback runbook for #5385 re-materializes the
// old ids with the old binary and an automatic reaper would fight it.
//
// Every executed request records the eshu_dp_workload_legacy_retirements_total
// counter by outcome for every node it judged, one INFO line, one WARN line
// per kept node, and a
// governance audit event. Dry runs record none of these.
package retire
