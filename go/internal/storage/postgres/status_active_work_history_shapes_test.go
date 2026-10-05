// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// historyFenceCase holds the rows the grouped history pass must fence the
// same way the per-row join did. Hand count, on scope sa (active ga2) and sb
// (active gb1):
//
//   - visible: h-old-succeeded (terminal rows ignore the stale fence),
//     h-superseded, h-quarantined, h-new-failed, h-claimed (overdue),
//     h-running (stale but leased), h-retrying, h-pending (ga3 wins the tie),
//     sb's two succeeded rows and one pending row;
//   - hidden by the stale fence: h-old-failed, h-old-dead;
//   - hidden by the scope/generation join: h-mismatch-succeeded and
//     h-mismatch-quarantined (scope sa, generation gb1 of scope sb).
//
// total 15 counts every row; succeeded is 3 (the mismatch row is not one).
func historyFenceCase() historyCase {
	row := func(id, scope, gen, stage, status string) historyWork {
		return historyWork{
			id: id, scope: scope, gen: gen, stage: stage, domain: "hist", status: status,
			created: historyCreated, updated: historyUpdated,
		}
	}
	failed := func(w historyWork, text string) historyWork {
		w.failureMessage = text
		return w
	}
	leased := func(w historyWork, key, until string) historyWork {
		w.conflictKey, w.claimUntil = key, until
		return w
	}
	return historyCase{
		name: "history_rows_fence",
		asOf: historyEdgeAsOf,
		seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
			staleScopeGenerations(ctx, t, conn)
			historyWorkRows(ctx, t, conn,
				row("h-old-succeeded", "sa", "ga0", "reducer", "succeeded"),
				failed(row("h-old-failed", "sa", "ga0", "reducer", "failed"), "stale-fail"),
				failed(row("h-old-dead", "sa", "ga1", "reducer", "dead_letter"), "stale-dead"),
				failed(row("h-new-failed", "sa", "ga2", "reducer", "failed"), "live-fail"),
				row("h-mismatch-succeeded", "sa", "gb1", "reducer", "succeeded"),
				row("h-mismatch-quarantined", "sa", "gb1", "reducer", "quarantined"),
				row("h-quarantined", "sa", "ga2", "reducer", "quarantined"),
				row("h-superseded", "sa", "ga0", "projector", "superseded"),
				leased(row("h-claimed", "sa", "ga2", "reducer", "claimed"), "hk-claimed", "2026-10-05 02:00:00+00"),
				leased(row("h-running", "sa", "ga0", "reducer", "running"), "hk-running", "2026-10-05 03:00:00+00"),
				failed(row("h-retrying", "sa", "ga2", "reducer", "retrying"), ""),
				row("h-pending", "sa", "ga3", "reducer", "pending"),
				row("h-sb-succeeded-1", "sb", "gb1", "reducer", "succeeded"),
				row("h-sb-succeeded-2", "sb", "gb1", "projector", "succeeded"),
				row("h-sb-pending", "sb", "gb1", "reducer", "pending"))
		},
		expect: []historyExpectation{
			{"queue", 1, "total_count", "15"},
			{"queue", 1, "succeeded_count", "3"},
			{"queue", 1, "failed_count", "1"},
			{"queue", 1, "dead_letter_count", "0"},
			{"queue", 1, "in_flight_count", "2"},
			{"queue", 1, "overdue_claim_count", "1"},
			{"queue", 1, "outstanding_count", "5"},
			{"queue", 1, "retrying_count", "1"},
			// projector|succeeded, projector|superseded, reducer|claimed,
			// reducer|failed, reducer|pending, reducer|quarantined,
			// reducer|retrying, reducer|running, reducer|succeeded.
			{"stage", 0, "", "9"},
			{"stage", 6, "status", `"quarantined"`},
			{"stage", 6, "count", "1"},
			{"stage", 9, "status", `"succeeded"`},
			{"stage", 9, "count", "2"},
			{"failure", 1, "work_item_id", `"h-new-failed"`},
		},
	}
}

// queueFailureProvenanceCase is the shim's queue_failure_provenance case: all
// six live and failed statuses, an overdue claim, blank failure text, and
// provenance-required rows with the upgrade marker present.
func queueFailureProvenanceCase() historyCase {
	row := func(id, domain, status string, provenance bool, updated string) historyWork {
		return historyWork{
			id: id, scope: "sx", gen: "gx", stage: "reducer", domain: domain, status: status,
			provenance: provenance, created: historyCreated, updated: updated,
		}
	}
	return historyCase{
		name: "queue_failure_provenance",
		asOf: historyEdgeAsOf,
		seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
			historyScopes(ctx, t, conn, [2]string{"sx", "gx"})
			historyGenerations(ctx, t, conn, [4]string{"gx", "sx", "2026-10-05 01:00:00+00", "active"})
			retry := row("a-retry", "package_source_correlation", "retrying", true, historyUpdated)
			retry.failureClass = "retry-class"
			fail := row("b-fail", "container_image_identity", "failed", true, historyUpdated)
			fail.failureMessage = "failed-message"
			claim := row("c-claim", "container_image_identity", "claimed", true, "2026-10-05 01:59:00+00")
			claim.conflictKey, claim.claimUntil = "claim-key", "2026-10-05 02:00:00+00"
			running := row("d-running", "other", "running", true, "2026-10-05 01:59:00+00")
			running.conflictKey = "run-key"
			pending := row("e-pending", "package_source_correlation", "pending", true, "2026-10-05 01:59:00+00")
			pending.stage = "projector"
			blank := row("h-blank", "other", "retrying", false, "2026-10-05 02:00:01+00")
			blank.failureClass = "   "
			historyWorkRows(ctx, t, conn, retry, fail, claim, running, pending,
				row("f-succeed", "package_source_correlation", "succeeded", true, "2026-10-05 01:59:00+00"),
				row("g-dead", "package_source_correlation", "dead_letter", true, "2026-10-05 01:59:00+00"),
				blank)
			historyExec(ctx, t, conn, `INSERT INTO cross_scope_completion_upgrade_markers (marker_name, applied_at)
VALUES ('provenance_edge_identity_upgrade_096', TIMESTAMPTZ '2026-10-05 01:00:00+00')
ON CONFLICT (marker_name) DO NOTHING`)
		},
		expect: []historyExpectation{
			{"queue", 1, "total_count", "8"},
			{"queue", 1, "outstanding_count", "5"},
			{"queue", 1, "overdue_claim_count", "1"},
			{"queue", 1, "provenance_edge_identity_upgrade_required", "2"},
			{"queue", 1, "provenance_edge_identity_upgrade_applied", "true"},
			{"failure", 1, "work_item_id", `"a-retry"`},
			{"failure", 0, "", "1"},
		},
	}
}

// backlogOrderLeaseOnlyCase is the shim's backlog_order_and_lease_only case:
// fact backlog order plus a shared domain with only a live lease.
func backlogOrderLeaseOnlyCase() historyCase {
	return historyCase{
		name: "backlog_order_and_lease_only",
		asOf: historyEdgeAsOf,
		seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
			historyScopes(ctx, t, conn, [2]string{"sl", "gl"})
			historyGenerations(ctx, t, conn, [4]string{"gl", "sl", "2026-10-05 01:00:00+00", "active"})
			row := func(id, domain, created string) historyWork {
				return historyWork{
					id: id, scope: "sl", gen: "gl", stage: "reducer", domain: domain,
					status: "pending", created: created, updated: created,
				}
			}
			historyWorkRows(ctx, t, conn,
				row("a1", "a", "2026-10-05 00:01:41+00"),
				row("a2", "a", "2026-10-05 01:01:41+00"),
				row("b1", "b", "2026-10-05 01:01:41+00"))
			for _, intent := range [][4]string{
				{"z-pending", "z", "2026-10-05 00:31:41+00", ""},
				{"z-completed", "z", "2026-10-05 00:01:41+00", "2026-10-05 01:00:00+00"},
				{"q-completed", "q", "2026-10-05 00:01:41+00", "2026-10-05 01:00:00+00"},
			} {
				historyExec(ctx, t, conn, `INSERT INTO shared_projection_intents (intent_id, projection_domain,
  partition_key, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ($1::text, $2::text, 'p', 'r', 'run', 'g', '{}'::jsonb, ($3::text)::timestamptz,
  NULLIF($4::text, '')::timestamptz)`, intent[0], intent[1], intent[2], intent[3])
			}
			for _, lease := range [][2]string{
				{"z", "2026-10-05 03:00:00+00"}, {"c", "2026-10-05 03:00:00+00"}, {"expired", "2026-10-05 02:00:00+00"},
			} {
				historyExec(ctx, t, conn, `INSERT INTO shared_projection_partition_leases (projection_domain,
  partition_id, partition_count, lease_owner, lease_expires_at, updated_at)
VALUES ($1::text, 0, 1, 'worker', ($2::text)::timestamptz, TIMESTAMPTZ '2026-10-05 02:00:00+00')`, lease[0], lease[1])
			}
		},
		expect: []historyExpectation{
			{"backlog", 0, "", "4"},
			{"backlog", 1, "domain", `"a"`},
			{"backlog", 1, "outstanding_count", "2"},
			{"backlog", 2, "domain", `"z"`},
			{"backlog", 2, "in_flight_count", "1"},
			{"backlog", 3, "domain", `"b"`},
			{"backlog", 4, "domain", `"c"`},
			{"backlog", 4, "outstanding_count", "0"},
			{"queue", 1, "total_count", "3"},
		},
	}
}

// blockageLeaseReadinessCase is the shim's blockage_lease_readiness_visibility
// case: a live conflict lease, one missing and one satisfied readiness phase,
// and a row not yet visible.
func blockageLeaseReadinessCase() historyCase {
	return historyCase{
		name: "blockage_lease_readiness_visibility",
		asOf: historyEdgeAsOf,
		seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
			historyScopes(ctx, t, conn, [2]string{"sr", "gr"})
			historyGenerations(ctx, t, conn, [4]string{"gr", "sr", "2026-10-05 01:00:00+00", "active"})
			row := func(id, domain, status, key, created string) historyWork {
				return historyWork{
					id: id, scope: "sr", gen: "gr", stage: "reducer", domain: domain,
					status: status, conflictKey: key, created: created, updated: created,
				}
			}
			holder := row("holder", "alpha", "claimed", "alpha-key", historyCreated)
			holder.claimUntil = "2026-10-05 03:00:00+00"
			needs := row("needs-phase", "gcp_relationship_materialization", "pending", "", "2026-10-05 00:31:41+00")
			needs.payload = `{"entity_key":"missing"}`
			has := row("has-phase", "gcp_relationship_materialization", "pending", "", "2026-10-05 00:31:41+00")
			has.payload = `{"entity_key":"good"}`
			future := row("future-visible", "hidden", "pending", "", "2026-10-05 00:31:41+00")
			future.visibleAt = "2026-10-05 03:00:00+00"
			historyWorkRows(ctx, t, conn, holder,
				row("blocked-one", "alpha", "pending", "alpha-key", "2026-10-05 00:01:41+00"),
				row("blocked-two", "alpha", "retrying", "alpha-key", historyCreated),
				needs, has, future)
			historyExec(ctx, t, conn, `INSERT INTO graph_projection_phase_state (scope_id, acceptance_unit_id,
  source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
VALUES ('sr', 'good', 'gr', 'gr', 'cloud_resource_uid', 'canonical_nodes_committed',
  TIMESTAMPTZ '2026-10-05 01:00:00+00', TIMESTAMPTZ '2026-10-05 01:00:00+00')`)
		},
		expect: []historyExpectation{
			{"blockage", 0, "", "2"},
			{"blockage", 1, "domain", `"alpha"`},
			{"blockage", 1, "blocked_count", "2"},
			{"blockage", 1, "conflict_key", `"alpha-key"`},
			{"blockage", 2, "domain", `"gcp_relationship_materialization"`},
			{"blockage", 2, "conflict_key", `"cloud_resource_uid:canonical_nodes_committed:missing"`},
		},
	}
}
