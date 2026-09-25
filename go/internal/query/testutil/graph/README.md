# testutil/graph

## Purpose

The graph half of `testutil`, nested under it for #6642: the graph-read
test doubles and the NornicDB Cypher-shape guards. See `doc.go` for the
godoc contract and `../README.md` for the consumer counts and split history
that stay with the parent.

## Ownership boundary

Same as the parent: helpers used by more than one package's tests, and no
production behavior. A helper used by one package belongs in that package's own
`_test.go` file.

## Exported surface

See `doc.go` for the godoc contract.

- `FakeGraphReader` — a graph-read double satisfying the two-method read port
  handlers depend on. Dispatches on query text: incoming-edge traversals go to
  `RunIncomingFn`, the dead-code scanner's paged candidate probe is answered
  with no rows, everything else goes to `RunFn`. The zero value is usable.
- `FakeGraphReaderWithSingle` — the same port with plain dispatch (`RunFn` /
  `RunSingleFn`, no query-text routing). Do not repoint its users at
  `FakeGraphReader`; the routing would silently change what they assert.
- `FakeRepoGraphReader` — a graph-read double for `getRepositoryContext`
  tests. Dispatches on the longest matching Cypher fragment in `RunByMatch` /
  `RunSingleByMatch`; `RunFn` / `RunSingleFn` override that entirely.
  `RunSingle` has a single-entry fallback: an unmatched narrow
  single-repository lookup (`MATCH (r:Repository {id: $repo_id})`) returns the
  sole registered row when exactly one is registered.
- `FakeWorkloadGraphReader` — the same dispatch shape for `getWorkloadContext`
  tests, deliberately without the single-entry fallback. See "Two near-duplicate
  fakes, not one type" below before touching either.
- `AssertCypherHasNoBrokenAndOr` (X4) and `AssertCypherHasNoIgnoredLabelPredicate`
  (X11) — the #6786 NornicDB v1.3.3 Cypher-shape guards; see `doc.go`.

### FakeGraphReader's dispatch, and why its fields are exported

`FakeGraphReader`'s `Run` routes through an unexported `rows` helper, and its
`RunSingle` falls back to that same helper rather than calling `Run` (it still
prefers `RunSingleFn` when one is set). `FakeRepoGraphReader` and
`FakeWorkloadGraphReader` reach the same end differently, by inlining their
dispatch in each method; neither has a `rows` helper. What matters is the
result, not the shape: the package stays free of any `Run`/`RunSingle`
call expression, which is what lets `internal/queryplan`'s callsite inventory
walk this directory instead of skipping it — see `../AGENTS.md`'s invariants.

An unexported field cannot be set from another package, so a type alias would
carry the type without the ability to fill it in. The `Fn` suffix keeps the
fields from colliding with the `Run` and `RunSingle` methods.

### How root uses FakeGraphReader without touching 154 files

Root keeps an unexported `fakeGraphReader` adapter whose fields have the old
lowercase names, and whose methods delegate to `FakeGraphReader`. 155 root files
build it with keyed literals; one of them,
`code_relationships_graph_test.go`, is where the adapter is declared, so 154
consume it and none of those 154 changed.

The delegation is proven rather than assumed. Deleting the incoming-edge
dispatch from this package fails **10** root tests; a narrower mutation that
keeps the branch but ignores `RunIncomingFn` fails 5. Both measured by running
the whole root suite, 8324 tests, against a baseline of 0 failures.

Those two mutations are close enough to be worth distinguishing. Deleting the
branch lets an incoming-edge query fall through to `RunFn`; keeping the branch
but dropping the `RunIncomingFn` call leaves it answering no rows. Collapsing
the branch to `return nil, nil` is the SAME edit as the second, not the first,
and reports 5 rather than 10 -- two mutations that are secretly one mutation
read as corroboration and are not.

Failure counts are top-level test functions; run totals are `=== RUN` lines
pinned to a named `origin/main` commit. The re-measurement protocol is in the
parent `../AGENTS.md` under Common changes.

### Two near-duplicate fakes, not one type

`FakeRepoGraphReader` and `FakeWorkloadGraphReader` were promoted the same
way, from `repository_context_test.go` and `workload_context_test.go`. Root
keeps the same kind of unexported adapter (`fakeRepoGraphReader`,
`fakeWorkloadGraphReader`) for each. Only the file that used to declare each
fake changed; the other 42 and 29 consuming test files are untouched.

Both counts are measured on `.go` files only, and with the `{` of a composite
literal rather than the bare name. That distinction is worth keeping: a bare
`rg fakeRepoGraphReader` returns 44 where `fakeRepoGraphReader{` returns 43,
because `workload_context_test.go` names the fake in two doc comments without
ever building one. Counting mentions instead of constructions invents a
consumer, which has produced a wrong number here more than once.

The two fakes look alike -- same fields, same longest-fragment dispatch -- but
they are separate types on purpose. `FakeRepoGraphReader.RunSingle` falls back
to a sole registered row when the narrow single-repository lookup
(`MATCH (r:Repository {id: $repo_id})`) does not match anything registered.
`FakeWorkloadGraphReader.RunSingle` has no such fallback: `getWorkloadContext`
has no equivalent single-entity lookup, and adding one would hand a workload
test a row it never registered. Unifying the two behind a shared type -- even
one gated by a flag -- would give every workload test the repository
fallback's behavior. Workload tests would keep compiling, and most would keep
passing, because their `RunSingleByMatch` maps happen to have more than one
entry or their fragments happen to match. The fallback would misfire silently
for the ones that do not.

Both delegations are proven the same way `FakeGraphReader`'s is: break the
rule in `testutil/graph`, run the whole root suite, restore, confirm green
again.

- Deleting `FakeRepoGraphReader`'s single-entry fallback fails **16** root
  tests.
- Short-circuiting `FakeWorkloadGraphReader.RunSingle`'s `RunSingleByMatch`
  dispatch to `nil, nil` fails **40** root tests.

Both measured against the same 8324-run, 0-failure baseline as every
other proof in this package's docs, on this branch rebased onto `origin/main`
460c59481. That total is not a portable constant: it grows as tests are added
and shrinks when a family moves out of root, as `semanticsearch` did.
Re-measure rather than carrying one forward -- the counts in this file were
carried forward three separate times before anyone noticed the sections
disagreed.

## Dependencies

Leaf packages only, as the parent's invariant 2 states. This package does not
import the parent `testutil` or its sibling leaf.

## Telemetry

None. A test double deliberately emits no telemetry.

## Gotchas / invariants

A non-test file under `internal/query` importing this package fails
`internal/queryplan`'s callsite inventory, the same as an import of the parent.

## Related docs

`../README.md`, `../AGENTS.md`.

## Performance and observability

No-Regression Evidence: this package is a test double and runs only inside test
binaries -- `internal/queryplan`'s callsite inventory walks it and records zero
`Run`/`RunSingle` call expressions, so nothing here is on a production query or
graph path. `FakeRepoGraphReader` and `FakeWorkloadGraphReader` trip the
perf-evidence gate on the word "Cypher" in their doc comments, which describe
which Cypher FRAGMENT a caller registers, not a query this code issues. The
promotion is a move: the dispatch bodies came across from
`repository_context_test.go` and `workload_context_test.go` unchanged, so the
dispatch work per call is identical; the adapter adds one struct construction
per call, in test binaries only. Root suite: 8324 `=== RUN`, 0 `--- FAIL`, on
this branch rebased onto `origin/main` 460c59481.

No-Observability-Change: no metric, span, log, or status surface is touched. A
test double deliberately emits no telemetry -- one that produced spans would
pollute the traces of whatever it stands in for.
