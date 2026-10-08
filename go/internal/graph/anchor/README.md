# Anchor

`anchor` states which id-bearing graph nodes the labeled Neo4j entity-context
anchor can reach, and the two checks that keep that set closed. Issue #7212.

## Where this fits

```mermaid
flowchart LR
  S["graph schema\nUID / ID constrained labels"] --> L["anchor.Labels"]
  R["replay capture\n(JSONL recordings)"] --> W["anchor.CheckWriters"]
  L --> W
  G["go source literals"] --> W
  W --> P["golden-corpus-gate\nphase writer-coverage"]
  L --> C["anchor.CensusCypher"]
  C --> Q["golden-corpus-gate\ngraph/anchor_census (Neo4j leg)"]
  C --> M["reducer maintenance runner\nperiodic gauge"]
```

## The definition

A node is reachable when it has an `id` and either sits on an id-constrained
label, or sits on a uid-constrained label with `uid = id`. A node counts once.
`Classify` is that definition in Go; `CensusCypher` is the same definition as
one AllNodesScan, and `census_live_test.go` (tag `live_nornicdb_answer_truth`)
holds the two together on a real Neo4j.

## Files

```text
anchor/
  doc.go               package contract
  labels.go            UIDLabels, IDLabels, Labels (schema-derived)
  writers.go           Statement, Finding, Report, CheckWriters
  parse.go             Cypher scan: clauses, node patterns, SET items
  proof.go             parameter proof that a dynamic map has no id key
  census.go            Classify, CensusCypher, Census, EvaluateCensus
  reader.go            ReaderCensus: the census over a single-row graph read port
  sweep_test.go        static sweep of go/ Cypher literals, planted violations
  sweep_literals_test.go  which literals the sweep admits and how it folds them
  sweep_allowlist_test.go  the named list of dynamic-label writers
  census_live_test.go  live Neo4j proof of the census Cypher
```

## What CheckWriters does and does not see

It reads Cypher text. Over a replay recording it sees what ran, including
labels chosen at run time.

Over Go source the static sweep admits a string literal, or a `+` chain of
literals and non-literal operands (a non-literal operand reads as `%s`, like a
fmt verb), that has a write keyword (`MERGE`, `CREATE`, `SET`), an `id` token or
a `+=` map write, and a node pattern: labeled (`(n:Label`), unlabeled with an
`id` key in its map (`(n {id: ...})`), or with a placeholder label
(`(n:%s`). DDL (`CREATE CONSTRAINT/INDEX ... FOR (n:%s)`) is skipped. Static
labels go through `CheckWriters`. A placeholder-label writer cannot be decided
statically, so each one must be a named row in `sweep_allowlist_test.go`, with a
reason and the proof that covers it. A new dynamic-label writer, or an edit to a
listed template, fails the sweep until the row is added or re-read. The 13
listed writers are the canonical and semantic entity upsert templates, the
`internal/graph` batch helpers, and the read-API latency seed tool.

What stays uncovered is a dynamic-label writer the corpus replay does not
execute and the sweep cannot see (a label built by a function call the fold
cannot read, or a statement assembled outside a `+` chain or a fmt template). The
census check and the census gauge are the backstop for that class.

## Operational notes

- `CensusCypher` is one AllNodesScan. On 2026-10-08, image sha-57167b0, it took
  about 1.95 s over 1,130,424 nodes (898,874 with an id). Run it on an interval
  with a timeout, never on a request or scrape path.
- The census is one read transaction, not a point in time. A node written
  during the scan may or may not be counted.
- `coalesce(n.uid = n.id, false)` is load-bearing. Without it, a null uid makes
  `NOT (null AND ...)` null, and the node drops out of the residual.

## Telemetry

None here. The periodic gauge and its log line live in
`go/internal/reducer/maintenance` and `go/cmd/reducer`.
