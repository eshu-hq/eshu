<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2026 eshu-hq -->

# Removal-tail PREP: satellite-to-satellite rollback rehearsal plan draft (#9669)

Refs #9669, part of #6707. Status: draft only for code-deletion
rollback — the pin-level rehearsal already ran under D12 (reverse-pin
PR restored the last-good pins, re-verified truth/availability/state,
restore PR re-applied the dest pins; evidence on #9669). D12 proves
satellite pin rollback, not rollback of a code deletion; the removal
PR must record its own disposition (revert PR plus prior core digest)
on #9669 before it merges, updating this draft.

Precedent: the #9665/#9670 arbiter ruling
(`docs/internal/evidence/arbiter-9665-9670-ruling.md`, Decision 1–2) and
the webhook-satellite roll-forward runbook
(`docs/public/operate/webhook-satellite-roll-forward.md`).

## Plan A: satellite-to-satellite rollback (prior digest restore)

1. Name the pre-removal revision: TBD — the removal PR's own base SHA
   (writing it earlier would be stale on arrival, per the arbiter ruling
   Decision 2).
2. Restore the prior satellite digest: TBD — digest(s) of the last
   serving eshu-api image(s) before the removal-half change.
3. Re-verify truth: TBD — parity-harness subset (envelope, pagination,
   capability/profile gating, authz) re-run against the restored digest
   with named corpus revisions.
4. Re-verify availability: TBD — `/healthz`, `/readyz`, and the latency
   budgets (`testdata/benchmarks/read-api-route-*.txt`) against the
   restored topology (topology: TBD — deployments, services, ingress that
   route to the satellite).
5. Re-verify durable state: TBD — no durable writes live in the satellite
   (mutations stay core-owned via admission contracts), so the check is
   that core Postgres/graph state is untouched by the restore; evidence
   artifact: TBD.
6. Record evidence: TBD — links to the rehearsal run, digests, and
   verification outputs, filed on #9669 before the removal PR merges.

## Plan B: owner roll-forward waiver (alternative, if the owner chooses it)

If the owner waives rollback rehearsal per #9383, the removal PR must
instead carry, in the same PR:

1. An explicit owner waiver statement quoting #9383's second disposition
   (roll-forward waiver **plus a filed revival follow-up** — the waiver
   alone is not a disposition).
2. A filed revival follow-up issue (TBD number) owning the satellite
   revival procedure, named in the PR body — mirroring the #9665 pattern
   where the follow-up issue is the waiver's own condition.
3. The pre-removal revision (the removal PR's base SHA) recorded in that
   follow-up issue, since only the removal PR fixes its base.

Either Plan A (rehearsed, evidence linked on #9669) or Plan B (waiver +
filed follow-up, both quoted in the removal PR) must be complete before
the removal PR merges. This draft decides neither; the owner does.
