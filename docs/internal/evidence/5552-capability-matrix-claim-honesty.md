# Capability matrix claim-honesty runtime evidence (#5552)

Validation date: 2026-08-09.

This record covers the shared runtime and operator proof behind the production
capability-matrix requalification. The generated inventory maps 110 supported
remote-validation slugs to 115 production row occurrences. One hundred legacy
slugs use the shared golden-corpus driver; the component-extension pair and
dead-IaC slug use their dedicated deployed drivers. Every per-slug artifact
records its own capability-specific response assertion.

## Shared deployed run

The accepted shared run used the 30-repository local-full-stack corpus, rebuilt
host binaries, Postgres, and the pinned NornicDB image
`eshu-nornicdb-pr290:3722b483c02c` (upstream revision
`3722b483c02c38a8e046d198f8768f200f31023c`, based on the repository's
v1.1.11 compatibility target).

```bash
GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
```

Captured output: `0`.

The run completed with 547 passing assertions, zero required failures, and
zero advisory warnings. Its atomic terminal snapshots reported zero residual
or dead-letter fact work items, zero nonterminal shared-projection intents, and
zero nonterminal cross-scope completion events. The retained stack was then
removed with its volumes and orphans.

## Performance and reliability disposition

Performance Evidence: the deployed run completed in 133 seconds against the
driver's 15-minute reference baseline and 30-minute blocking ceiling, using the
same 30-repository input shape and pinned backend described above. The result is
an end-to-end no-regression measurement for the changed projection, graph-read,
Postgres, and MCP paths. It is not a scaled throughput claim.

No-Regression Evidence: focused failing-first tests cover the corrected
projection metadata, dependency-direction traversal, package and security
reads, service/documentation SQL, and golden response contracts. The deployed
run then exercised the rebuilt binaries through fact replay, terminal drains,
NornicDB materialization, Postgres reads, and HTTP/MCP responses. Dedicated
component-extension and dead-IaC runs cover the three slugs outside the shared
driver; their exact commands and direct exit captures remain in their per-slug
artifacts.

No-Observability-Change: the corrected paths add no metric, label, queue,
worker, lease, or retry policy. Projector metadata promotion continues through
the existing `canonical_write` stage, covered by
`eshu_dp_projector_stage_duration_seconds`, `eshu_dp_canonical_writes_total`,
and `eshu_dp_canonical_projection_duration_seconds`. Query and Postgres work
continues through the existing request and database duration/error signals.
The B-7 phase timings, structured stage logs, and atomic drain summaries remain
the operator-facing evidence that the deployed pipeline reached terminal
state.
