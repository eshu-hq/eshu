# Evidence: inheritance materialization rc-12 diagnostic counters (#3873)

Scope: hot-path reducer domain `inheritance_materialization` — adds two
diagnostic counters (`content_entity_facts`, `entities_with_declared_parent`)
logged on every handler exit path and a helper (`countInheritanceFactInputs`) in
a new sibling file. No write-path, query, or edge-resolution behavior changes.

## Why

rc-12 (`(Class)-[:INHERITS]->(Class)`) intermittently projects 0 on loaded CI
but is green everywhere reproducible: corpus-gate passed on the recent merges,
and the B-7 golden corpus gate is stable at rc-12 count=6 across 6 runs on three
environments — local arm64 (clean ×2, amplified workers ×1) and remote amd64
16-core (amplified workers ×3) — never reproducing count=0. The inheritance edge
already has a readiness gate (`DomainInheritanceEdges` gates on
`canonical_nodes_committed`, #2867), and the drain quarantines no domain as
advisory, so it waits for every shared_projection_intents domain (including the
inheritance edges) to reach terminal before asserting. An rc-12=0 therefore means
the inheritance handler completed but resolved few/zero edges.

Because the flake does not reproduce in any available environment, the next step
to a correct root-cause fix is in-situ capture. These counters make the next CI
occurrence diagnosable from logs alone: a low `content_entity_facts` count points
to a partial upstream fact set (ordering stall), while `entities_with_declared_parent > 0`
with `edge_count = 0` points to declared parents that resolved to no in-corpus
entity rather than a missing-fact stall. They do not change the green, gated
write path (no speculative change to behavior that has not been proven broken).

## No-Regression Evidence

No-Regression Evidence: the only added work is one O(loaded-envelopes) counting
pass over facts the handler has already loaded into memory — no graph round trip,
no new query, no new fact load, no change to intent emission or edge resolution.
One extra log line with two integer counters is emitted per intent. B-7 golden
corpus gate green with the change present: rc-12 count=6, 51 checks pass, ~32s on
the remote amd64 host and ~37s locally — unchanged from the pre-change runs.
`go test ./internal/reducer -run Inheritance -count=1` passes, including the new
`TestCountInheritanceFactInputs`.

## Observability Evidence

Observability Evidence: a new `inheritance materialization fact inputs` log line
is emitted BEFORE the empty/stall early-return branches, so it covers every exit
path (not just the success path). It carries `content_entity_facts`,
`entities_with_declared_parent`, `edge_count`, `repo_count`, and
`projection_contexts`, so an operator can distinguish the two rc-12=0 failure
modes (partial fact set vs unresolved declared parents) from one log line without
a reproduction — including the empty/stall exits where the success completion log
never fires. No metric, span, or status field is removed.
