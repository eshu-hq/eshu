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

`Run` and `RunSingle` both route through an unexported `rows` helper rather than
`RunSingle` calling `Run`. That keeps the package free of any `Run`/`RunSingle`
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

## Dependencies

`testing` and the standard library today. Leaf packages such as
`internal/status`, `internal/governanceaudit`, or `queryauth` are allowed when a
fake genuinely needs the types it stands in for.

Root `internal/query` and the handler families are not, and that one is not a
convention: root imports every family for its compatibility aliases, so
importing either from here is an import cycle and the package stops building.

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
