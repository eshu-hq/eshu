# #8903: trimmed-twin retirement of padded leftovers (proof note)

Option A per `/tmp/8903-ruling.txt`: the delete judges a padded leftover
against its trimmed twin. Go (`TwinID`, own-repository comparisons in
`evaluate.go`) and the Cypher gate (`cypher.go`) trim together. Unchanged: the
legacy-shape clause (byte for byte), lock-first re-check, instance gates, the
safe-relationship gate, generation/`DEFINES` requirements, Neo4j-only refusal,
dry-run-first. No production execute happened or is claimed here: the only
deletions were fixture nodes in a disposable test container.

## Backend scope: Neo4j only

Pinned `neo4j:2026-community` in an isolated compose stack, Bolt only, fresh
volume. Nothing here validates any other backend.

## Proof 1: live seed flipped (leading/trailing name, padded repo id)

`seedPadded` writes three eligible padded leftovers beside trimmed twins:
leading-space name, trailing-space name, both-sides-padded repo id (all ASCII
space). `TestLiveDeleteNeo4jRemovesPaddedLeftovers` deletes over every fixture
id and observes exactly 4 nodes (p1 + the three padded) and 5 relationships
(p1 DEFINES + SAME_NAME, one DEFINES per padded leftover); the four twins
report count 1 with one DEFINES each; all seven padded negatives survive; a
second run deletes 0; `Detect` then reports 16 verdicts, 0 eligible.

## Proof 2: Go and Cypher agree; one-side trim fails

`TestLiveDetectionAndDeleteStatementSelectTheSameIDs` passes with the padded
ids selected by both sides. Mutation check (Cypher reverted to byte-for-byte
joins, Go still trimmed): the same test fails with `Detect selects
[workload: padname-… workload:p1-… workload:padrepo-… workload:padtrail-…]
but the delete statement selects [workload:p1-…]`. The reverse direction (Go
raw) fails the flipped unit tests (`TestTwinID`,
`TestEvaluatePaddedNameOrRepoUsesTrimmedTwin` fail red pre-implementation).

## Proof 3: negative controls stay blocked

Seven padded negatives, all leading-space: instance by edge and by property
report `has_instances`; DEPENDS_ON edge reports `relationships`; twin absent,
twin without `generation_id`, twin the repository does not DEFINE, and twin in
a different repository only all report `no_twin`. Asserted via `Detect` in the
equality test and surviving the real delete in the removal test.

## Proof 4: trim equivalence stated explicitly

Live probe `RETURN trim($in)` vs Go `strings.TrimSpace` on Neo4j 2026.08.1:
space, tab, LF, CR, and U+200A agree; U+00A0 (no-break space) does NOT (Go
strips it, Cypher keeps it). Claim scope is ASCII whitespace; the live seed
proves space only. U+00A0 is out of claim scope in code comments, runbook,
package README, and readiness doc — never silently assumed.

## Proof 5: plan still seeks

`TestLiveDeleteNeo4jPlanSeeksTheIDConstraint` passes on the trimmed statement.
Observed PROFILE operators: `NodeUniqueIndexSeek(Locking)` (batch anchor),
`NodeIndexSeek` (repository anchor on the trimmed repo id),
`NodeUniqueIndexSeek(Locking)` (twin lookup on the computed trimmed twin id);
no `AllNodesScan`, `NodeByLabelScan`, or `NodeIndexScan`. The computed trimmed
twin id seeks; shape proven, wall time at production scale not claimed.

## Proof 6: first production use (not run here)

Per the runbook: owner go-ahead, reviewed dry run naming the padded ids,
execute in a quiet window on one reducer pod, recount via dry run until no
eligible candidates remain, readiness re-check. Retry story unchanged.

## Readiness behavior change (PR-body material)

Once a padded node can have a twin, `WithScopedTwin` counts it, so
`workload_identity_leftovers` FAILS where it previously passed while padded
nodes were present. Suggested PR body note: "Behavior change: padded-name and
padded-repo-id leftovers are now counted as leftovers with a scoped twin and
fail `workload_identity_leftovers` until retired; this is the correct truth
for the padded-repo case, which causes real by-name ambiguity. No readiness
special-casing was added."

## Markers

No-Regression Evidence (#8903): correctness-only widening of the twin match
from byte-for-byte to trimmed on both sides; plan shape pinned seeking by
`TestLiveDeleteNeo4jPlanSeeksTheIDConstraint`; full package suite (unit plus
live, `-tags live_nornicdb_answer_truth`) green on an isolated Neo4j; no
latency/throughput claim.

No-Observability-Change (#8903): no counter, log line, audit event, span, or
route contract changed (`runner.go` untouched; existing `outcome` labels and
WARN lines reused). Detection verdict strings unchanged.

## What stays unverified

Whether any padded node exists on a real deployment (first reviewed dry run
answers that); statement wall time at production graph scale; property-only
references outside `WorkloadInstance` remain unchecked, as the runbook warns.
