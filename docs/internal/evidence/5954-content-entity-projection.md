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

Observed on a live `golden-corpus-gate` run against this branch (elapsed
148s):

| label | count |
| --- | --- |
| `TerraformBlock` | 5 |
| `CloudFormationCondition` | 2 |
| `CloudFormationExport` | 3 |
| `CloudFormationImport` | 2 |
| `PagerDutyDeclaration` | 1 |
| `Repository` | 31 |

All six match the snapshot's pinned ranges/notes exactly, and the
`GET /api/v0/iac/resources` query-shape assertion (`summary.total=23`) passed.
Gate summary: 554 pass, 0 required-fail, 1 advisory-warn.

**Advisory-warn disposition.** The one advisory warning was a
`phase_graph_query` timing check. This run cannot separate that warning from
host load on the machine it ran on — it is a single sample on a loaded host,
not a quiet-host measurement, so it is not evidence of anything the diff did.
The mechanism argument for why it is unlikely to be the diff: this change adds
one 5-file repo and 13 projected nodes total, no snapshot query shape fans out
over these labels, and the IaC inventory CTE is restricted to three Terraform
entity types so it gains exactly one row from this change. That argument
supports not attributing the warning to the diff; it does not prove host load
caused it either, since no quiet-host rerun exists yet. A quiet-host rerun
would be the second sample needed to say more than "not separable from host
load on this run."
