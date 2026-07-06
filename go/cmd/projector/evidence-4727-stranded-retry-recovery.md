# Evidence: always-on projector service recovers stranded source-local retries (#4727 / #3624)

## Root cause (topology gap, not a missing mechanism)

Bootstrap-index projects via the durable Postgres projector queue (`cmd/bootstrap-index/wiring.go:104`), enqueuing the source-local work item in the SAME tx as the generation+facts (`internal/storage/postgres/ingestion.go:305-316`). On a retryable canonical-write failure it routes the item to `Fail` → `status='retrying'` with backoff `visible_at` (`projector_queue.go:281-345`), leaving the generation `pending`, then drain-exits rc=1 (`bootstrap_projector.go:122-127`). In `docker-compose.yaml` the ONLY continuous source-local claimer (the ingester) is gated `bootstrap-index: service_completed_successfully`, so after a bootstrap failure the stranded, perfectly-claimable `retrying` item has NO claimer forever — its generation never activates, and other scopes' inheritance/sql intents defer on readiness that needs the failed scope's canonical nodes (~1645 wedged cross-scope, dead_letter=0). Helm does not have this class (its ingester StatefulSet is ungated); the remote-e2e foundation already ships a standalone projector.

## Fix (E1)

Add the existing `eshu-projector` service to `docker-compose.yaml`, gated ONLY on
db-migrate + workspace-setup (NOT bootstrap-index) — matching the Helm ungated
ingester and the remote-e2e topology. A stranded `retrying` item is always
claimed, re-projected end-to-end (cmd/projector wires the full Runtime incl.
PhasePublisher/RepairQueue), and its generation activated → cross-scope readiness
unblocks. Zero new Go recovery logic; reuses the already-tested claim SQL
(FOR UPDATE SKIP LOCKED, supersede, expired-lease reclaim). The projector's
graph-write budget defaults to 2 (`${ESHU_GRAPH_WRITE_MAX_IN_FLIGHT:-2}`): the only
window it co-claims with the primary bootstrap writer is the bootstrap-overlap
window (combined-writer accounting incl. the concurrent reducer is in Performance
Evidence below, cleared by the 0-timeout ladder). After bootstrap exits — the only
state in which a stranded item actually exists to recover — the projector is the
sole source-local canonical writer and its 2-permit budget is pure headroom.

## Proof

Performance Evidence: recovery is a scheduling/topology fix (an always-on
claimer), not a hot-path write change; it does not alter any query shape. The
added projector is a tertiary canonical writer whose per-process in-flight budget
defaults to 2. Budgets are independent per-process semaphores: the projector's +2
is a stage subtotal, not the full ceiling — the resolution-engine (reducer,
budget 8) is also gated only on db-migrate+workspace-setup and can co-write, so
the pre-existing theoretical worst case was already bootstrap (8) + reducer (8) =
16 (top of the knee) and this change raises the theoretical ceiling to 18 only if
all three saturate simultaneously. That arithmetic is superseded by the
load-bearing measurement below: the reducer consumes only *activated* generations
and during healthy cold-ingest bootstrap holds the source-local leases, so the
three writers do not in practice all saturate at once. Measured no-regression on the
25-repo cold-ingest ladder WITH the projector service co-claiming alongside
bootstrap (`docker compose -p cap25nr`, SHA e68270625, clean volume): stream-
complete +240s, queue-zero +246s, canonical-write failures 0, graph-write 30s
timeouts 0 (bootstrap and projector), dead_letter 0, and full convergence
(25/25 generations active, 25/25 scopes carry active_generation_id) with the
projector exiting rc=0. This matches the without-projector baseline (queue-zero,
0 failures): the added co-claiming writer at budget 2 introduces no regression on
the healthy path.

No-Regression Evidence: DSN-gated Postgres regression
`TestProjectorStrandedRetryRecovery` / `...LeaveLiveLease`
(`internal/storage/postgres/projector_stranded_retry_recovery_test.go`, gated on
`ESHU_GENERATION_LIVENESS_PROOF_DSN`), run PASS on a scratch Postgres:
- FAILING side: `GenerationLivenessStore.RecoverWedgedGenerations` recovers 0 from
  the constructed stranded-pending state (its active-only / own-intents /
  in-flight gates each exclude it) — pins that no existing mechanism drains it.
- GREEN side: an ungated `NewProjectorQueue(db, "projector", ...)` `Claim` returns
  the stranded item and `Ack` activates its generation (`status='active'`,
  `activated_at` set, `ingestion_scopes.active_generation_id` set) — the recovery
  the compose projector service performs continuously.
- NEGATIVE: a source-local item under a LIVE lease (`claim_until` in the future,
  i.e. bootstrap actively projecting) is NOT claimed — no re-drive of in-flight
  work; the double-projection race is excluded by the existing heartbeated lease.

No-Observability-Change: no new instruments; the projector service exposes the
same `/healthz`, `/readyz`, and `/metrics` surface cmd/projector already ships, and its claim
telemetry reuses the existing projector-queue signals.

Deferred follow-up (tracked): a bounded liveness arm for the dead-letter/poison
class (item dead_letter + gen 'failed', no newer gen), reusing the
liveness_recovery_attempts LEAST-cap + write-time re-verify discipline, plus
stuck-gauge coverage so this wedge class alarms. Only sound after this fix (its
output needs a claimer). Concurrency-critical liveness SQL deserving its own
#4464-grade proof — not bundled here.
