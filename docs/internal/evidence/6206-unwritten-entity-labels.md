# 6206 — pinning the entity labels that are registered but have no source-local writer

## What changed

Nothing that runs. This change is comments, package docs, two new test files,
and one CI gate trigger. It records WHY `Variable` (and the rest of the
unwritten set) is registered in `entityTypeLabelMap` while phase E of
`canonical_builder.go` deliberately never writes it, and it pins that set so the
next reader cannot quietly "fix" the inconsistency in either direction.

The trap this closes: reading a row in `entityTypeLabelMap` as "these nodes
exist."

REPORTED, not re-run here — two committed figures record what the unwritten
state actually is. The comment on `entityTypeLabelMap` in
`go/internal/projector/canonical.go` records a live golden-corpus run
measuring `(Variable) count=0`, with no `Variable` key in `graph.node_counts`
at all. That zero and the label's reachability are not in conflict, and the
comment now says so: the only `Variable` writer left is the reducer's
semantic-entity path, which accepts Elixir module attributes and TSX
component-type assertions, and the golden corpus stages no Elixir or TSX
fixture (`scripts/lib/golden-corpus-fixtures.sh`). Nothing in that corpus can
match, so the count is zero for corpus reasons, not because the writer is
dead. The `contentEntityBuckets` row in
`go/internal/content/shape/materialize_tables.go` records why that skip is
worth keeping: at corpus scale `Variable` was by far the largest entity
family, at **12,887 chunks and 21,515s of cumulative graph-write time**
(cited there to `go/internal/projector/README.md`). Writing those nodes is
the expensive direction, and this change does not take it. The row is load-bearing for other
reasons — `Variable` carries a uid constraint in `graph/schema_tables.go`, the
variables bucket in `content/shape` materializes the label, and
`EntityTypeLabel` is the resolver the #5531 three-way bucket-sync gate reads —
so deleting it reds three separate tests. Both the "delete the row" and the
"start writing the nodes" reactions are wrong, and now both are pinned.

## No-Regression Evidence:

This change has no production code path to regress.

VERIFIED — the two files the gate flags as hot contain no executable change.
`go/internal/collector/gitrepo/git_snapshot_entity_buckets.go` has **zero**
non-comment changed lines:

```
git diff origin/main...HEAD -- go/internal/collector/gitrepo/git_snapshot_entity_buckets.go \
  | rg '^[+-]' | rg -v '^[+-]\s*//' | rg -v '^(\+\+\+|---)' | rg -v '^[+-]\s*$'
```

returns nothing.

VERIFIED — `go/internal/projector/canonical.go` is semantically identical.
Stripping comments and collapsing runs of spaces, the old and new files are
byte-identical; its three "changed" lines are gofmt realignment of existing map
entries after a comment was inserted above them:

```
git show origin/main:go/internal/projector/canonical.go | rg -v '^\s*//' | tr -s ' ' > old
rg -v '^\s*//' go/internal/projector/canonical.go       | tr -s ' ' > new
diff old new    # exit 0
```

VERIFIED — `go/internal/content/shape/materialize_tables.go` likewise has zero
non-comment changed lines. The remaining diff is `README.md`, `AGENTS.md`, two
`_test.go` files, and the ci-gates registry.

Backend/version: not applicable — no Cypher, no query plan, no schema change,
and no graph or Postgres statement in this diff. Terminal row and queue counts
are unchanged because no statement is issued and no node label starts or stops
being written.

## No-Observability-Change:

Nothing operator-facing moves.

No metric, span, or log is added, removed, or renamed. Which rows become
`Variable` nodes is exactly what it was before this change — plain source
variables no, Elixir module attributes and TSX component-type assertions yes —
and `graph.node_counts` carried no `Variable` key on the golden corpus before
this change and carries none after, because that corpus stages neither
language. An operator sees exactly the same series with the same values.

Accepting no new telemetry is reasonable because this change adds no runtime
behaviour to observe. What it adds is a build-time guard, and a drift between
the registered labels and the written ones now fails at test time rather than
being discovered from a puzzling zero in a corpus run.

## Why the change is safe

The unwritten set is pinned by `canonicalEntityPhaseSkipOwners` in
`canonical_unwritten_entity_labels_test.go`. If a future change starts writing
one of those labels, or stops writing one that is currently written, the pin
fails and names the label. The three tests that already depend on the
`Variable` row — `TestEntityTypeLabelMapCoversAllSchemaLabels`,
`TestEntityTypeLabelHandlesBothCases`, and content/shape's
`TestContentEntityLabelsHaveProjectorLabels` — continue to guard the delete
direction.
