# OpenAPI Package Move — Evidence (#6642 part C)

Moves the 114 non-test `openapi*.go` files out of the `internal/query`
root into `internal/query/openapi/`. The original #6060 plan froze this
set as "stays root" on the reasoning that `OpenAPISpec()` concatenates
unexported package-level consts and a Go package boundary follows the
directory boundary. A `go/types` census disproved the premise: the set
declares 118 consts and 5 funcs, no types, no vars and no methods, and
uses nothing from the rest of `package query`. Exporting the constants
per leaf removes the only thing pinning them.

Shape is decided by the dirgate 40-file cap, not by taste: 101 of the
114 are path fragments, so they nest as `openapi/paths/<family>/` (16
fragment leaves plus the `paths/supply/` documentation namespace once the
owner's later `supply/chain` and `code/dead` nests landed; largest leaf is
`paths/supply/chain` at 18 files including `doc.go`). `openapi/schema/` exists
because `openAPIImpactRuntimeTopologyLimits` is spliced into both a
components file in the parent and a paths fragment in a leaf, and the
parent imports the leaves, so a fragment both need cannot live in
either. The 63 `openapi_*_test.go` files stay in root: they reach into
root unexported internals, and they only call `OpenAPISpec()` and
`ServeOpenAPI`, which root keeps as forwards in `handler.go`.

## Wire contract

Byte-identical assembled spec. 948,356 bytes, sha256
`850615ac7265a187ed26372792f248d6f87169bd9abce4959dbf9858482d49ed`
before and after the move; `cmp` exit 0. The 118 new constant names were
renamed one at a time against the rendered JSON they produce, not by a
blanket rule, because a rename rule that silently drops or collides on one
name changes the published document and no test named after this diff
would catch it. The byte-identical spec is the proof: any dropped or
mis-renamed constant would have changed the assembled document or failed
to compile.

`scripts/verify-openapi.sh` reports `255 HandleFunc routes, 255 OpenAPI
path entries` on both `origin/main` and the branch.

## Performance and observability

No-Regression Evidence: behavior-preserving by construction — the move
rewrites each file's `package` clause and constant names and nothing
else, and the fragment bodies are untouched. `go build ./...` exit 0,
`go vet ./internal/query/...` exit 0, `go test ./internal/query/...
-count=1` exit 0 across 45 packages. Test inventory, re-measured at the
final rebased head: `go test ./internal/query/... -list '.*'` yields 4,802
`Test*` names on `origin/main` and 4,807 on the branch — a net +5, not a
loss. `comm -23` (base-only, i.e. dropped) is empty; the 5 branch-only
names are the owner-approved security fix's new coverage:
`TestSAMLHandlerACSRejectsBackslashReturnToPath`,
`TestOIDCLoginHandler{Start,Callback}RejectsBackslash*`, and
`TestGitHubLoginHandler{Start,Callback}RejectsBackslash*`. The openapi move
itself renames no test and drops none; every test-count delta traces to
that later, separately-evidenced security commit.

No-Observability-Change: no span, metric, tracer, log field or pprof
identifier is added or renamed. No route, operation ID, capability
string or response shape changes.

## Gate changes the move forced

`scripts/verify-openapi.sh` globbed `"$query_dir"/openapi_paths_*.go` at
depth 1 and excluded fragments from the HandleFunc scan by the basename
`openapi_*.go`. After the move the glob matched nothing (run on the
branch before the fix: exit 1, every route reported `MISSING_OPENAPI`)
and the basename exclusion stopped matching, since the new basenames are
`routes.go`, `tokens.go` and so on. The fragment scan is now recursive
over both the flat and the nested layout, fail-closed on an `rg` hard
error the same way the Go file scan already was, and the exclusion has a
directory form alongside the basename one.

`scripts/test-verify-openapi-subpackage.sh` gains a third vector: a
fragment that has moved into `openapi/paths/<family>/` must still be
found. It bites — with the fragment recursion disabled the suite reports
`3 tests, 2 passed, 1 failed` on exactly that vector, and returns to 3/3
when it is restored.

`internal/query`'s dirgate row is re-pinned down to 361 files, digest
`a31cec4ac6d97abe6cb5305155e05e19f3aaa8a6a488e479058c5ae1d39e4d34`
(re-derived at the final rebased head onto `origin/main`, whose own
intervening commits — including #6060's later moves of the language,
secrets, package-registry, service, workitem, visualization and freshness
families out of the query root — kept shrinking the row between
rebases; the 114-file delta this PR's own move accounts for is
unchanged each time). The digest run reports zero remaining
`openapi`-prefixed naming violations, which is the mechanical proof that
every non-test `openapi*.go` left root: the naming-exempt ledger only
shrinks, so a straggler could not have been grandfathered.
