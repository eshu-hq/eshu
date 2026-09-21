# internal/reducer/code/value/affected

## Purpose

Answers the value-flow refresh emit gate (issue #6785): which repos own at
least one Function calling a cloud action, constrained by producer-written
keys (repo ids, workload ids, or principal uids). A producer run whose rows
touch no cloud-calling repo can never grow a cloud sink, so its ACK emits no
completion event.

## Ownership boundary

**Owns:** the three gate statements and their row parsing
(`ReposWithCloudCallers*`).

**Does not own:** the producer handlers that extract the keys, the ACK SQL
that reads the reported count, or the fixpoint solver itself (`code/value`).

## Performance evidence

No-Regression Evidence: the emit gate adds one UNWIND-first single-hop
repo-count read per producing run, only when that run wrote rows, plus one
coalesced singleton refresh item per completion event set — no new hot-path
scan. B-7 golden-corpus runs on the same 20-repo corpus, the same pinned
backend (timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f),
and the same host show no regression: baseline at 112be588e ran
bootstrap 6s / collect 1s / first_drain 129s / maintenance_drains 82s /
graph_query 3s (238s total); after the refresh work at 5dabba4b0 the same
phases ran 6s / 1s / 68s / 82s / 4s (185s total), with 565 pass at baseline
and 568 pass after (0 required-fail both) against the 1800s budget ceiling.
Drain-time variance across runs dominates the delta; graph-query time is
flat.

Observability Evidence: every gate evaluation records one
eshu_dp_value_flow_refresh_gate_evaluations_total point labeled by producer
domain and outcome (affected/suppressed/fail_open) and one
reducer.value_flow_refresh_gate span carrying the outcome and
affected_repo_count, so an operator can see at 3 AM whether refreshes are
firing, suppressed, or failing open per producer.
