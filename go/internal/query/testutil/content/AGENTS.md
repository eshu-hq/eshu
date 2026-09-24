# testutil/content — agent instructions

## Read first

- `doc.go` — what lives here.
- `../AGENTS.md` — the invariants every `testutil` package follows.

## Adapting the content-read fakes without churning callers

`fakePortContentStore` is the one that needed the structural move first. Its
fields were typed with read models declared unexported in root, so no adapter
could name them from another package; those read models moved to `querycontract`
before the fake could follow. 125 root files name it and one declares it, so 124
consume it -- the same "declarer excluded" convention the parent README uses
for its 84 and 80 -- while 93 build one with a composite literal. Count
constructions, not mentions.

Measure that root-only. A git pathspec of `go/internal/query/*.go` crosses a
directory separator and returns 126, because the registry family's own double
names root's in a comment. That file is not a call site.

Its adapter forwards through a single `promoted()` converter rather than
mapping its 29 fields by hand at each call site. That is the shape to copy
for any fake wide enough that per-field mapping would be written more than once.

`fakeDeadCodeContentStore` followed, and was the last one. It needed the same
shape for a smaller reason: only `deadCodeIncomingEdge` was unexported in root,
in one read's return type, so it moved to `querycontract` behind a root alias
first. 35 root files build it, and every file that names it also builds one.

No shared double remains in root for the content-read family. A family moving
out of `internal/query` can now reach every one of them from here.

The content-reader SQL driver (`OpenReaderTestDB`, `ReaderQueryResult`, and the
column helpers) came across the same way, with one wrinkle worth copying. Its
entry point takes a **slice** of the result struct, and 80 root test files
build those elements with keyed literals over lowercase field names. So root
keeps its own unexported struct with the original names and converts the slice
element by element before delegating. Nothing else moved into root: the queue,
the default answers, and the assertions live only in the driver package,
because two copies of a fake's dispatch drift and the drifted one keeps
passing. That package is `internal/testutil/contentreader` since #6818 move 5;
the file names there drop the `reader_` prefix (`args.go`, `columns.go`,
`defaults.go`, `driver.go`).

If you find yourself editing consuming test files while moving a fake, the
shape is wrong. Go back to the adapter — see the parent `../AGENTS.md` for the
general rule and its one narrow exception.

## Watch for a fake blocked by its own signatures

`fakePortContentStore` was blocked by its own signatures before it could move.
Its fields and methods named unexported root read models
(`repositoryEntryPointReadModel`, `documentationFindingListReadModel`, and 14
more), so no adapter could have moved it; the types had to reach `querycontract`
first, with root keeping an alias for each. Four already-exported root types
needed the same treatment for a less obvious reason: exported is not enough when
the only way to name them is to import package `query`. That import is the one
leg of the parent's invariant 2 the compiler actually catches, and it catches it
only in a test binary — root's own tests import `testutil`, so the cycle
surfaces when `internal/query`'s tests build. `go build
./internal/query/testutil/content` still succeeds on its own, so a green
build would not have told you.

Two production symbols moved with it. `k8sSelectCandidateFromEntity` had exactly
one caller, this fake, so it went to `querycontract` rather than being copied;
`k8sNamespace` followed because that projection needs it, and a second copy of a
trim that namespace equality gates SELECTS matching on is a drift risk, not a
convenience.

## The narrow-optional-port warning

When a fake answers a narrow optional port — one package `query` reaches by
type-asserting the store rather than through `ContentStore` — a signature that
stops matching does not fail to compile. The assertion goes false and the
handler takes its fallback path, so every test still passes. Mutate one of
those methods as part of the delegation proof, not just a `ContentStore` one.
