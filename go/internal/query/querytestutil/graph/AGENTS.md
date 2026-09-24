# querytestutil/graph — agent instructions

## Read first

- `doc.go` — what lives here.
- `../AGENTS.md` — the invariants every `querytestutil` package follows.

## Invariant 4, applied to the graph-read fakes

The parent's invariant 4 (no `Run` or `RunSingle` call in a non-test file) is
enforced across this leaf the same way it is across the parent. The three
graph-read fakes here are the only ones with those two methods, and they
satisfy it two different ways: `FakeGraphReader` routes both methods through
an unexported `rows` helper, while `FakeRepoGraphReader` and
`FakeWorkloadGraphReader` inline their dispatch in each method. Either is
fine. What a new fake must not do is have one of the two methods call the
other.

## Near-duplicate fakes are not one type

`FakeRepoGraphReader` and `FakeWorkloadGraphReader` promoted the same way
`FakeGraphReader` did, from `repository_context_test.go` and
`workload_context_test.go`. They dispatch the same way — longest matching
Cypher fragment wins in `RunByMatch`/`RunSingleByMatch`, `RunFn`/`RunSingleFn`
override everything — and it is tempting to fold them into one type, maybe
with a bool field for the difference.

Do not. `FakeRepoGraphReader.RunSingle` has a fallback the workload fake does
not: when no fragment matches, the cypher is the narrow single-repository
lookup (`MATCH (r:Repository {id: $repo_id})`), and exactly one row is
registered, it returns that row. `FakeWorkloadGraphReader.RunSingle` returns
nil in the same situation, on purpose — `getWorkloadContext` has no
single-entity lookup for a fallback to stand in for. Unifying the two types
would give every workload test the repository fallback too. Workload tests
would keep compiling, and most would keep passing, because most workload
`RunSingleByMatch` maps have more than one entry or their fragments actually
match — the fallback would only misfire for the ones that do not, silently
handing back an unregistered row instead of nil.

Both delegations are proven the same way `FakeGraphReader`'s was: break the
rule in `querytestutil/graph`, run the whole root suite, restore it, confirm
green again.

- Deleting the single-entry `RunSingle` fallback from `FakeRepoGraphReader`
  fails **16** root tests.
- Deleting the `RunSingleByMatch` dispatch from `FakeWorkloadGraphReader`'s
  `RunSingle` (short-circuiting to `nil, nil`) fails **40** root tests.

Both measured on this branch rebased onto `origin/main` 460c59481: 8324 tests
run, 0 failing, the same baseline every other proof in this package's docs
cites. That total is not portable -- it moves in both directions as tests are
added and as families move out of root. Restore the file and re-run the
baseline before trusting either number — a proof that leaves the break in
place is not a proof of anything else in the suite.

## Changing a NornicDB shape guard

`nornicdb_guards.go` holds the #6786 X4 and X11 guards. Every RED case in
their seeded tests is a shape measured live on the pinned NornicDB build, and
every GREEN case is measured correct on it and on Neo4j; cite the evidence doc
for a new case rather than reasoning about the parser. The X11 guard's known
blind spots are listed on `IgnoredLabelPredicate` and pinned by
`TestIgnoredLabelPredicateDocumentedBlindSpots`: when a change closes one, move
that case into the RED set and update the doc comment in the same commit.

## Anti-pattern

Merging two near-identical fakes into one type with a flag for the
difference. If you find yourself doing this to `FakeRepoGraphReader` and
`FakeWorkloadGraphReader`, stop — see "Near-duplicate fakes are not one
type" above.
