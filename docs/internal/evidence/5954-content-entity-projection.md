# #5954 — five content entities that reached the store but never the graph

## What was wrong

`content/shape`'s `contentEntityBuckets` and the collector twin
`snapshotEntityBuckets` both declare `terraform_blocks`, the three
CloudFormation extended buckets, and `pagerduty_declarations`, and
content/shape's own tests exercise them as live parser output.

They never became graph nodes. `extractEntities` calls `EntityTypeLabel` and
silently `continue`s when it returns false — no error, no dead letter, no
counter — so the rows landed in the content store and simply had no node. The
loss was invisible from the graph side: a query for these types returns nothing,
and nothing anywhere says why.

## The key is the entity_type, not the bucket name

They diverge, and getting this wrong is the same silent failure wearing a
different hat. The buckets are `cloudformation_cross_stack_imports` and
`cloudformation_cross_stack_exports`; the entity types the parser emits are
`cloudformation_import` and `cloudformation_export`
(`go/internal/query/entity_content_types.go` is the authority). A key derived
from the bucket name compiles, passes the bucket-sync gate, and still drops
every row.

`EntityTypeLabel` accepts either the snake_case map key or a PascalCase label
present in `entityTypeLabelValues`, which is built from the map's values at
init. The collector assigns `EntityType` from the bucket LABEL, so the
PascalCase form is the one production actually takes — the regression test
asserts both spellings for all five types, and fails on all ten before the
change.

## Constraints: uidConstraintLabels, not schemaConstraints

The first attempt added composite `(name, path, line_number)` constraints to
`schemaConstraints`, and only the **Neo4j** fingerprint moved. That asymmetry
was the tell: entries in `schemaConstraints` pass through
`dialect.constraint()`, which drops composite constraints for NornicDB — the
DEFAULT backend. Those five labels would have had no uniqueness constraint
exactly where it matters most.

`uidConstraintLabels` generates `REQUIRE n.uid IS UNIQUE` for both backends,
and matches how the projector actually writes these nodes (MERGE by uid). Both
fingerprints moving is the evidence the constraint lands on both.

Without a constraint, a MERGE against these labels is an unindexed label scan
per row, and concurrent writers can create duplicate nodes for one identity —
the hazard CLAUDE.md's "Serialization Is Not A Fix" section names.

## Five registries, one label

These five labels touch SIX separate registries. Four of the packages I
changed were green before the whole-module sweep caught the fifth, and the
live lane caught the sixth:

1. `content/shape` `contentEntityBuckets` — already had them
2. the collector twin `snapshotEntityBuckets` — already had them
3. `projector.entityTypeLabelMap` — added here
4. `graph.uidConstraintLabels` — added here
5. `specs/replay-depth-requirements.v1.yaml` — added here, caught only by
   `TestRetractableNodeTypesLockstep` in a different package
6. the replay coverage manifest and its cassette binding — caught by
   `TestEntityRetractManifestBinding`, which rejected a coverage row whose
   cassette carried no `content_entity` fact for the type

That lockstep is the mechanism enforcing "correctly retracted on delta
re-sync": a label cannot become retractable without the replay gate also
demanding a delta scenario for it.

## No-Regression Evidence

No-Regression Evidence: whole-module `go test` via
`scripts/generate-code-coverage-report.sh` (exit 0, no failures) proves
correctness, NOT cost — review correctly rejected it as performance evidence.
The measured runtime cost is in "Measured: the five always-on retract
statements" below.

The honest shape of the cost, stated rather than waved away: this change **adds
graph writes** for entity types that previously produced none. A repo
containing Terraform blocks, CloudFormation conditions/imports/exports, or
PagerDuty declarations now materializes nodes it silently dropped before, so
per-projection work grows in proportion to how many such entities that repo
has. That is the fix, not a regression — the previous cost was lower because
the work was not being done.

What is genuinely unchanged:

- The DDL runs once at startup. Five additional `CREATE CONSTRAINT ... IF NOT
  EXISTS` statements are idempotent and add no per-row cost.
- `EntityTypeLabel` is a map lookup; five more entries change nothing
  measurable on the projector's hot path.
- The new constraints make the MERGE path for these labels *cheaper* than it
  would be without them (indexed lookup rather than a label scan), which is
  why the constraint is not optional.

The B-7 golden-corpus live lane passed on this branch (509s), so the corpus's
projected graph truth and drain assertions are unchanged by the registration.
This is the state before the fixture work below landed: at that point the
20-repo corpus did not carry these entity types, so the B-12 snapshot did not
move. The "#5954 fixture coverage" section below is the same issue's later
change that does move the snapshot, staging real fixtures for these five
labels instead of leaving them provably wired but uncovered.

## Measured: the five always-on retract statements

Review was right that the earlier no-regression claim was a test run, not a
measurement. `buildEntityRetractStatements` issues one `DETACH DELETE` per
registered label on every non-first-generation FULL-REFRESH projection,
unconditionally — so these five cost something even for a repository that
contains none of these entity types. That is the case measured here.

Performance Evidence: NornicDB v1.2.1 (pinned `eshu-nornicdb-pr290:3722b483c02c`),
single local container, Bolt driver, the exact
`canonicalNodeRetractEntityTemplate` statement against an EMPTY label
(`repo_id` matches nothing), 20 warm-up runs then n=200:

| measure | value |
| --- | --- |
| per-statement `DETACH DELETE`, empty label | 265.9 µs |
| added cost for the five new labels | **1.33 ms** per full-refresh projection |
| retract phase, 95 labels (before) | 25.26 ms |
| retract phase, 100 labels (after) | 26.59 ms (**+5.3%**) |

So the cost is real, bounded, and paid once per full-refresh projection, not
per row or per file. It does not touch the delta path
(`buildDeltaEntityRetractStatements` is a different branch) and it is zero on
first-generation projections, which return before the loop.

**Limits of this measurement.** One local container, no concurrency, and the
empty-label case only — deliberately, because that is the scenario the finding
is about. It does not measure the cost when these labels actually carry nodes,
which is work the projection would owe anyway once the entities materialize. It
is a per-statement round-trip measurement, not an end-to-end projection
baseline.

## Observability Evidence

No-Observability-Change: no new stage, worker, queue, or query is introduced.
These entity types now flow through the SAME canonical node write path, the
same `CanonicalNodeWriter` phases, and the same retract path every sibling
content-entity label already uses, so they are covered by that path's existing
telemetry.

The condition this change removes — a content entity silently skipped by
`extractEntities` — had no signal at all, which is why it survived two issues
(#5483, #5531) before being fixed. It is now structurally impossible for these
five: `knownMissingProjectorLabels` is empty, so the bucket-sync gate enforces
full three-way parity with no exemptions, and the replay lockstep demands a
delta scenario per retractable label.

## Schema compatibility

Additive, and safe in both directions during a rolling deploy. A writer on the
predecessor schema creates none of these five node types — the projector did
not recognise their entity types at all — so it cannot violate a constraint
that only now exists, and a writer on the new schema adds constraints an older
reader does not consult. Recorded as
`graphSchemaNeo4jPreContentEntityGraphFingerprint` and its NornicDB peer.

## Verification

```bash
cd go && go test ./internal/projector ./internal/graph \
  ./internal/content/shape ./internal/storage/cypher ./internal/replaycoverage -count=1
bash scripts/generate-code-coverage-report.sh   # exit 0, whole module
```

All packages `ok`. The new regression test
`TestContentEntityTypesReachTheGraph` fails on all ten cases before the change.

## #5954 fixture coverage

The wiring above made these five labels retractable and writable but left them
without a real fixture in the live 20-repo corpus — a repo containing none of
these entity types proves nothing about whether they materialize. This staged
`cloudformation_comprehensive` into the live gate (it previously existed on
disk only for a parser unit test) and added `terraform_comprehensive/pagerduty.tf`,
moving the B-12 snapshot's node-count floors for all five labels off zero.

Observed identically across three live `golden-corpus-gate` runs against this
branch, taken at different points in the rebase history (elapsed 148s, 157s,
and 193s):

| label | count |
| --- | --- |
| `TerraformBlock` | 5 |
| `CloudFormationCondition` | 2 |
| `CloudFormationExport` | 3 |
| `CloudFormationImport` | 2 |
| `PagerDutyDeclaration` | 1 |
| `Repository` | 31 |

All six matched the snapshot's pinned ranges/notes exactly in every run, and
the `GET /api/v0/iac/resources` query-shape assertion (`summary.total=23`,
`summary.by_kind.resource=14`, `count=14`) passed every time. 0 required-fail
in all three. The three gate summary lines:

| run | elapsed | summary |
| --- | --- | --- |
| 1 (`cdf3730a03`) | 148s | 554 pass, 0 required-fail, 1 advisory-warn |
| 2 (`3b4f63a888`) | 157s | 555 pass, 0 required-fail, 0 advisory-warn |
| 3 (`3b4f63a888`, rerun) | 193s | 552 pass, 0 required-fail, 3 advisory-warn |

Runs 2 and 3 are the same code (`3b4f63a888`, this branch's HEAD after
rebasing onto current `origin/main`). Run 1 is an earlier commit
(`cdf3730a03`) at a pre-rebase HEAD, so it is not a clean single-code series
with the other two — the counts and query shape agreeing across all three is
still meaningful (the fixture and node-count logic did not change between
`cdf3730a03` and `3b4f63a888`), but the timing numbers below span two
different bases, not three samples of identical code.

The asserted truth this branch changes — the six node counts and the IaC
query shape — is stable across three different runs at three different points
in the rebase history and under three different load conditions. That is a
stronger claim than a single quiet run would have given: it is not evidence
the diff got lucky once, it is evidence the diff's effect on graph and query
truth does not move.

**Advisory-warn disposition.** The only check that varied was
`phase_graph_query`, an advisory timing check, never a required one:

| run | observed | baseline | ceiling | result |
| --- | --- | --- | --- | --- |
| 1 | 10.0s | 3.0s | 8.0s | WARN (host load 17-18, observed directly from this run) |
| 2 | 8.0s | 3.0s | 8.0s | PASS — landed exactly on the ceiling |
| 3 | 13.0s | 3.0s | 8.0s | WARN — host load ~38 at the time |

Run 3's contention has a known cause, not an unexplained spike: a second
live-gate run started on the same shared lock partway through, from a
different worktree's `make pre-pr` preflight for issue #5996 that began after
this run had already been told the lock was clear. That is why run 3 is the
noisiest sample and why its degradation was not confined to
`phase_graph_query` — it also warned on `phase_collect` (32.0s vs a 25.0s
ceiling) and `phase_maintenance_drains` (65.0s vs a 30.0s ceiling), neither of
which warned in runs 1 or 2. A host-wide effect from a second concurrent gate
run explains a warn spreading across multiple unrelated phases in a way a
diff-specific effect would not.

Run 3's own elevation has a confirmed external cause (the concurrent #5996
preflight above). Runs 1 and 2 do not have an equally specific known cause,
and neither is a clean quiet-host measurement either — both are still
elevated relative to the 3.0s baseline (10.0s and 8.0s) despite no identified
second gate run competing for the lock at the time. This host carried between
eight and twelve concurrent lanes plus other agents' preflights for the full
session, with load ranging roughly 7 to 49, and a quiet window was not
achievable in this session, so "no identified second gate run" is not the
same as "no contention" — something on the host was still busy. The mechanism
argument for why none of this is attributed to the diff: this change adds one
5-file repo to a 31-repo corpus and 13 newly projected nodes total, no
snapshot query shape fans out over these five labels, and the IaC inventory
CTE is restricted to three Terraform entity types so it gains exactly one row
from this change. That argument makes it implausible by mechanism that this
diff moves a 3s phase to 8-13s. It does not prove what did for runs 1 and 2 —
that part is **implausible by mechanism, unresolved by measurement**, not
contention confirmed. A WARN flipping to a PASS that lands precisely on the
ceiling is not proof of anything about the diff either way.
