# querytestutil

## Purpose

Test helpers reachable from `internal/query` and, once they exist, its
handler-family subpackages. Split out during the #6060 family moves.

Today there is exactly one consuming package, root `query`'s tests. See
`AGENTS.md` for why a single-consumer helper lives here and why that is not
precedent for the next one.

## Ownership boundary

Owns helpers used by more than one package's tests. A helper used by exactly
one package belongs in that package's own `_test.go` file, not here.

This package owns no production behavior and must not grow any. Anything that
production code needs belongs in `querycontract`.

## Exported surface

See `doc.go` for the godoc contract.

- `MustMapField` — walks a decoded JSON/OpenAPI document one key at a time,
  failing with the offending key name.
- `FakeGraphReader` — a graph-read double satisfying the two-method read port
  handlers depend on. Dispatches on query text: incoming-edge traversals go to
  `RunIncomingFn`, the dead-code scanner's paged candidate probe is answered
  with no rows, everything else goes to `RunFn`. The zero value is usable.
- `FakeRepoGraphReader` — a graph-read double for `getRepositoryContext`
  tests. Dispatches on the longest matching Cypher fragment in `RunByMatch` /
  `RunSingleByMatch`; `RunFn` / `RunSingleFn` override that entirely.
  `RunSingle` has a single-entry fallback: an unmatched narrow
  single-repository lookup (`MATCH (r:Repository {id: $repo_id})`) returns the
  sole registered row when exactly one is registered.
- `FakeWorkloadGraphReader` — the same dispatch shape for `getWorkloadContext`
  tests, deliberately without the single-entry fallback. See "Two near-duplicate
  fakes, not one type" below before touching either.

`Run` routes through an unexported `rows` helper, and `RunSingle` falls back to
that same helper rather than calling `Run` (it still prefers `RunSingleFn` when
one is set). That keeps the package free of any `Run`/`RunSingle`
call expression, which is what lets `internal/queryplan`'s callsite inventory
walk this directory instead of skipping it — see the invariants below.

### Why FakeGraphReader's fields are exported

An unexported field cannot be set from another package, so a type alias would
carry the type without the ability to fill it in. The `Fn` suffix keeps the
fields from colliding with the `Run` and `RunSingle` methods.

### How root uses it without touching 155 files

Root keeps an unexported `fakeGraphReader` adapter whose fields have the old
lowercase names, and whose methods delegate to `FakeGraphReader`. 155 test files
in package `query` build it with keyed literals and none of them changed.

The dispatch rules are not duplicated in that adapter, and that is the point.
Two copies drift, and a fake that no longer matches the real port keeps passing
while guarding nothing.

The delegation is proven rather than assumed. Deleting the incoming-edge
dispatch from this package fails **10** root tests; a narrower mutation that
keeps the branch but ignores `RunIncomingFn` fails 5. Both measured by running
the whole root suite, 6539 tests, against a baseline of 0 failures.

Measure that set with a full `go test ./internal/query/`, never with `-run`
naming the tests you expect. `-run` measures your own filter: the first attempt
here named four tests, saw four failures, and reported four as though it were
the dependent set.

Prefer this shape for the remaining shared fakes. Renaming fields across every
consumer is the alternative, and it buys nothing the adapter does not.

### Two near-duplicate fakes, not one type

`FakeRepoGraphReader` and `FakeWorkloadGraphReader` were promoted the same
way, from `repository_context_test.go` and `workload_context_test.go`. Root
keeps the same kind of unexported adapter (`fakeRepoGraphReader`,
`fakeWorkloadGraphReader`) for each. Only the file that used to declare each
fake changed; the other 43 and 29 consuming test files are untouched.

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
rule in querytestutil, run the whole root suite, restore, confirm green again.

- Deleting `FakeRepoGraphReader`'s single-entry fallback fails **16** root
  tests.
- Short-circuiting `FakeWorkloadGraphReader.RunSingle`'s `RunSingleByMatch`
  dispatch to `nil, nil` fails **40** root tests.

Both measured against the same 6539-test, 0-failure baseline as
`FakeGraphReader`'s proof.

## Dependencies

The standard library, plus any LEAF package a fake genuinely needs to name the
types it stands in for -- `internal/status`, `internal/governanceaudit` and
`queryauth` are the shape, not the list. Deliberately stated as a rule rather
than an inventory: each fake promoted here turns another leaf from allowed into
taken, and a sentence enumerating today's imports is stale the next time one
lands. Run `rg -l 'eshu-hq/eshu' --glob '*.go' --glob '!*_test.go' .` if you
want the current set; do not transcribe it here.

Root `internal/query` and the handler families are not. Both bans are real,
but they are not enforced the same way and neither stops `go build`:

- Root cycles as soon as root's own tests build, because root's tests import
  this package. `go build ./internal/query/querytestutil` still succeeds on
  its own, so a green build proves nothing here.
- A handler family compiles clean. A cycle appears only if that family's
  INTERNAL test package imports this one back; an external `_test` package
  never cycles at all.

`AGENTS.md` invariant 2 has the measured breakdown of all three shapes. Do
not settle the question with a green build.

A graph driver has no business here either. A fake answers from funcs a test
installs; if it needs a driver it is not a fake.

## Telemetry

None. Test-only package.

## Gotchas / invariants

Helpers here MUST live in ordinary `.go` files and be exported. Moving one into
a `_test.go` file makes it unreachable from every other package's tests and
silently undoes the split — the compiler reports it as an ordinary undefined
symbol in the consuming package, not as a packaging mistake.

This package is intended for tests only, and part of that is enforced rather
than observed. `internal/queryplan`'s `DiscoverQueryCallsites` walks this
directory like every other one under `internal/query`, so a `Run` or `RunSingle`
call landing in a non-test file here is an unregistered production query
callsite and fails
`TestHotCypherManifestCoversEveryProductionQueryCall` with the file named. The
fakes stay clear of that by routing `Run` and `RunSingle` through an unexported
helper, so the package holds no such call at all.

It did not always work that way. The inventory used to skip this directory,
because a fake whose `RunSingle` answers by calling `Run` looks exactly like a
production read to a syntactic walk. Paying for that skip meant a second set of
rules — standard-library imports only, and `Run`/`RunSingle` reached only from a
fake's own `Run`/`RunSingle` delegating to its receiver. Both were proxies for
"this cannot reach a backend", and both were wrong in a way that mattered: the
stdlib rule blocked fakes that legitimately need `internal/status`,
`internal/governanceaudit`, or `queryauth`, while the self-delegation whitelist
let a genuine graph read wearing that exact shape pass the gate in silence.
Removing the call removed the need for either.

The other enforced half is direction: no non-test file under `internal/query`
may import this package, because production code must not depend on test
doubles. Note its scope. It covers the tree the inventory walks, which is where
an importer would realistically appear, but this package sits under
`go/internal`, so anything under `go/` could import it and only convention stops
a package outside `internal/query` from doing so.

While that holds, the linker drops this package from production binaries. That
is a consequence of the invariant rather than a guarantee on its own: a
production import would quietly pull `testing` into a shipped binary, so it is
a defect to fix, not a fact to document.

## Related docs

`go/internal/query/README.md` for the family-split contract.
