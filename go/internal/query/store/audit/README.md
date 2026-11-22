# Query store audit (package audit)

## Purpose

Every capability in `specs/capability-matrix.v1.yaml` declares the stores that
answer its reads (`answering_stores`). Declaration completeness is checked by
`cmd/capability-inventory -mode verify`. This package checks that the
declarations are honest: it fails when a read uses a store the capability did
not declare. It is the layer-2 audit of the #10880 design record.

## How it works

- `Recorder` implements `db.ReadStore` and returns empty results. A probe in
  `probes_test.go` builds a production store on it and drives one branch, then
  reads back the SQL and arguments the store actually issued.
- `Classify` turns statements into store tokens (`fact_records_reducer`,
  `fact_records_collector`, `read_table:<table>`, `content_search`). `scrub`
  blanks comments and masks string literals, so `--` inside a literal stays a
  literal, and unquotes quoted identifiers. The classifier resolves fact kinds
  from literals, `IN` lists, and bind arguments; reads every item of a
  comma-separated `FROM` list; ignores CTE names, lifecycle joins, and
  PostgreSQL system catalogs (`pg_*`, `information_schema`, schema
  introspection, not a store); and treats the winners freshness watermark as the
  winners store. `deadSpans` drops a branch only when a boolean constant
  really closes it, with SQL precedence (`AND` over `OR`).
- It fails closed. It reports as unresolved a `fact_records` read with no
  resolvable kind, a kind bind of a type it cannot read, a CTE that shadows a
  store name, a `FROM` item it cannot parse, unbalanced parentheses, and an
  unterminated literal or comment. `shapes_test.go` holds one seeded RED/GREEN
  case per shape; a shape the classifier misreads without knowing it is the
  residual risk those cases are there to shrink.
- `Undeclared` returns the tokens a declaration does not name.
- `Census` classifies the SQL string literals of every non-test Go file per
  package. It needs the set of tables the schema creates (`KnownRelations`) so a
  CTE alias or a word in prose never reads as a table. It is a package-union
  floor: it compares a package to the union of the stores its capabilities
  declare, so it cannot say which capability a literal serves, and a package
  serving capabilities with different declarations passes on the union. The
  per-capability claim comes from the probes only.

## Waivers

`testdata/probe-waivers.txt` lists capabilities with no probe yet, and
`testdata/census-waivers.txt` lists query packages with no census owner yet.
Both may only shrink. `CheckWaivers` (`coverage.go`) is the ratchet as a pure
function: `probeWaiverCeiling` and `censusWaiverCeiling` in `audit_test.go` pin
the lengths, and a stale, duplicate, or growing entry fails. Raising a ceiling
constant is a code edit that only review catches. Regenerate the lists
with `ESHU_UPDATE_STOREAUDIT_WAIVERS=1 go test ./internal/query/store/audit`,
which records what is unprobed or unowned today.

## Limits the classifier states

- Fact kinds are attributed per statement, not per relation: a statement that
  reads `fact_records` and another table with its own `fact_kind` comparison gets
  the union of the kinds. The classifier recognizes a kind predicate only as `=`
  a literal or a bind, `IN (literals and binds)`, and `= ANY($n)`. These fail
  closed: `LIKE`, `SIMILAR TO`, `<>`, `!=`, `~`, `fact_kind` as a bare argument
  of a function (`lower`, `starts_with`, `left`, `coalesce`), a `NOT IN` that
  names a kind the statement does not also read by name, and any `NOT LIKE`.
  These are not classified and the classifier does not see them, so each can hide
  a reducer-kind read beside a sibling kind literal: casts (`::text`, `CAST`),
  `COLLATE`, `||`, `position()`, `^@`, range comparisons, `= ANY(ARRAY[..])`,
  `IN (SELECT ..)`, `NOT (fact_kind = ..)`, `IS DISTINCT FROM`, a row compare,
  a parenthesized column, and a `NOT IN` on one alias whose kinds another alias
  reads positively.
- The kind canary (`canary_test.go`) closes that class for every probed
  statement: after the same scrub and dead-branch blanking, each live
  `fact_records` alias must carry a recognized positive kind predicate of its
  own, or be one of three pinned `fact_id` join-backs (the impact list's page
  re-read `source` and operator `override`, and the readiness statement's
  package-manifest `fact` re-read, each re-reading rows already selected by a
  statement that filters kind elsewhere). "Of its own" is enforced two ways. The
  predicate is searched in the ref's own scope, the innermost subquery that holds
  it (a parenthesized join tree is skipped) cut to its `UNION`, `INTERSECT`, or
  `EXCEPT` arm, so a reused alias name or another arm's predicate satisfies
  nothing. And it must restrict, as far as the canary reads: a depth-zero `AND`
  conjunct of the `WHERE` or `ON` clause that holds the match, climbing out of
  boolean groups that are themselves conjuncts. A predicate that is an `OR` arm,
  under `NOT`, inside a function or `CASE`, directly in the select list, or
  tested with `IS`, `<>`, a cast, or a concatenation does not count. A new probed
  statement with an alias that lacks one goes red. Known limits, all latent (no
  probed production statement uses them; the census of the 24 distinct probe
  statements is the evidence): a correlated predicate inside a nested subquery
  (`NOT EXISTS`, `OR EXISTS`, a select-list scalar subquery, `LEFT JOIN LATERAL`)
  counts as the ref's own predicate although it does not restrict the ref's rows,
  and an `ON` predicate counts when the ref is the preserved side of a `LEFT` or
  `FULL` join, where `ON` does not filter it. A correlated `EXISTS` conjunct is
  a real restriction and stays accepted. The canary reads only recorded
  statements, so it does not cover the 155 waived capabilities (their full
  statements are never recorded) or the census, which sees fragments; that gap and
  the burn-down of these limits belong to #11212.
- A subquery whose `WHERE` is always false (a literal `AND FALSE` conjunct with
  no `OR` to rescue it) returns no rows, so the tables it names are not counted.
  A compound subquery is read arm by arm (`UNION` when every arm is empty,
  `EXCEPT` when the first is, `INTERSECT` when any is); one that mixes set
  operators is never dropped. The impact readiness predicate closes its scanner
  receipt chain that way.
- A quoted identifier named `true` or `false` fails closed, because unquoting
  would read it as the boolean literal.

## What the audit claims

For the capabilities and variants the probes list, every statement a probe drives
reads a subset of the stores the capability declares. The readiness block of the
impact list and explain responses is probed too (repository, CVE, image
reference, and package anchors), so the readiness tables are declared. The
remaining capabilities are waived. Graph and content-store reads are not
recorded. #10880 acceptance criterion 1 is met for the listed capabilities only.

## Not covered

A probe sees only the branches it drives, and the census cannot tell which
capability a literal serves. Graph (`MATCH`) and content-store reads are not
recorded yet; those capabilities are waived until a graph recorder lands.
