# Query Package Moves Evidence (#6818)

Issue #6818 removes stuttering package names under `go/internal/query` by
moving packages to plain names. Each move rewrites import paths and package
qualifiers in the files that import the moved package. Some of those importers
are "hot" to `scripts/verify-performance-evidence.sh` because they contain
Cypher or goroutine fan-out, even though the move leaves their behavior
unchanged. This note records that evidence, with one section per move. Later
#6818 moves append a section here.

## `query/queryspan` to `query/tracing`

Baseline and after states are named by git tree and blob hashes, not by
commit. Those hashes come from content, so they stay the same when the branch
is rebased or squashed, and anyone can resolve them with `git cat-file`. The
old package is still on `origin/main`, so its hashes can be re-derived there.
Get the after hashes from the merged commit with `git rev-parse
<commit>:<path>`.

| State | Path | Object |
| --- | --- | --- |
| Before (tree) | `go/internal/query/queryspan` | `d4b0105f3919847a95c153481a5b96f9bddc713e` |
| After (tree) | `go/internal/query/tracing` | `4c63aa1d588144128c66bb6a07821478b790e93e` |
| Before (blob) | `go/internal/query/queryspan/handlerspan.go` | `8e433940753f43cc6de10ee131ba1aece051e34d` |
| After (blob) | `go/internal/query/tracing/handler.go` | `91e9df160dd12981bacb38a5e22e56bb1fcf47e2` |
| Before (blob) | `go/internal/query/impact/contract.go` | `06f23775ca861a26a4ade98ae8f212e58044237a` |
| After (blob) | `go/internal/query/impact/contract.go` | `85394ab62e44f16c25efbbe05b2cad8ac8b4e4d3` |
| Before (blob) | `go/internal/query/impact/resource_investigation.go` | `2c8a0487eb739d7927d0a9d6e9b7a8ffdfc4b36b` |
| After (blob) | `go/internal/query/impact/resource_investigation.go` | `9d18fc5dcfda8e15c02cb5c38ac28a74532a37d5` |

The before hashes are the same at the pre-rebase merge base `9e542becc` and at
the current merge base with `origin/main`. The move commits cited in earlier
drafts (`d2aeb0589`, `c594ca6ed`, `af9e66c82`) are pre-rebase references and
are not on the branch. No backend or version applies. The change is
compile-time only: no query runs differently, and no Postgres or NornicDB work
is involved.

The gate flagged two importers as hot:

- `go/internal/query/impact/contract.go` is hot for its Cypher text (the
  `MATCH (provider:Repository ...)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)`
  read).
- `go/internal/query/impact/resource_investigation.go` is hot for its bounded
  goroutine fan-out (`sync.WaitGroup`, `chan error`, three `go func()`).
  It has no Cypher text.

In both files the diff changes two lines: the import
`github.com/eshu-hq/eshu/go/internal/query/queryspan` becomes
`.../query/tracing`, and the `queryspan.` qualifier on
`StartHandlerSpanWith` and `HandlerTracer` becomes `tracing.`.

No-Regression Evidence: the Cypher text and the concurrency code are
unchanged. The proof applies the substitution to each before blob and checks
that the result hashes to the after blob:

```bash
check() { # check <before-blob> <after-blob>
  git cat-file -p "$1" \
    | sed 's#query/queryspan"#query/tracing"#; s/\([^A-Za-z0-9_]\)queryspan\./\1tracing./g; s/^queryspan\./tracing./g' \
    | git hash-object --stdin | cmp -s - <(printf '%s\n' "$2")
}
check 06f23775ca861a26a4ade98ae8f212e58044237a \
  85394ab62e44f16c25efbbe05b2cad8ac8b4e4d3 # contract.go: exit 0
check 2c8a0487eb739d7927d0a9d6e9b7a8ffdfc4b36b \
  9d18fc5dcfda8e15c02cb5c38ac28a74532a37d5 # resource_investigation.go: exit 0
```

That leaves no other changed byte, so no Cypher line and no goroutine,
channel, or WaitGroup line changed. A second check used `go/scanner` to
extract every string literal outside the import block, from both the base and
head versions of each file. It found the same list both times: 113 literals
in `contract.go` (sha256 prefix `a2ffd812bb2ee4ec` on both sides) and 38 in
`resource_investigation.go` (`fef564adc3f0b6ff` on both sides). The Cypher
statement is one of those literals, so the query sent to the graph is the
same. `go test ./internal/query/impact/... ./internal/query/tracing/...
./internal/query/language/... -count=1` passed (368 tests, exit 0). The
concurrency shape, fan-out width, and error channel capacity are unchanged, so
no performance measurement applies.

No-Observability-Change: the package move leaves the span name, attributes,
and tracer name unchanged. `tracing/handler.go` differs from
`queryspan/handlerspan.go` only in its package clause. `git diff
d4b0105f3919847a95c153481a5b96f9bddc713e
4c63aa1d588144128c66bb6a07821478b790e93e` compares the two package trees
directly: the Go files differ only in the package clause and the godoc lead,
and the Markdown files only in the package name, the renamed file, and a note
recording the move. The tracer is still
`otel.Tracer("eshu/go/internal/query")`, and the span still carries
`http.route`, `eshu.capability`, and `service.namespace`. Saved span queries
and dashboards that match on these names still work. The
`docs/public/observability/telemetry-coverage.md` row now points at
`handler.go`.

## Performance and observability evidence for the `selector` leaf

No-Regression Evidence: six files move from `queryselector/` to
`selector/` (four Go files plus the `doc.go`/`README.md`/`AGENTS.md`
trio), 45 importer files repoint the import path and the `queryselector.`
qualifier to `selector.`, and 11 of those importers rename a
`selector`-named parameter or local to `rawSelector` where it would
otherwise shadow the new package qualifier. No call site, argument,
allocation, or loop bound changes.

Before tree `04a017c57463c9697fe0a8346003b5208437a21a`
(`9cf27f072:go/internal/query/queryselector`), after tree
`57c21635d3ff595bb75cc9d0e0f4770c9c5ca516`
(`HEAD:go/internal/query/selector`). Moved-file blob pairs
(before → after):

- `doc.go`: `3bf21fd1` → `ba207755` (package clause plus the godoc
  `Package queryselector` lead line only)
- `selector.go`: `4b6b97a0` → `988a1e80` (package clause only)
- `entity_repo_identity.go`: `2a177fe0` → `2c8401a0` (package clause only)
- `entity_repo_identity_test.go`: `aaf3531b` → `92abee14` (package clause only)
- `AGENTS.md`: title line only; `README.md`: byte-identical.

Importer substitution proof, base `9cf27f072`, run from the worktree root:

```bash
git diff --name-only 9cf27f072 HEAD -- '*.go' \
 | while read f; do git cat-file -e "9cf27f072:$f" 2>/dev/null && echo "MOD $f"; done \
 | while read cls f; do
     rg -q rawSelector "$f" && continue
     git show "9cf27f072:$f" | sed 's/queryselector/selector/g' | cmp -s - "$f" && echo "CLEAN $f"
   done
```

32 importer files are byte-exact under `s/queryselector/selector/g`.
Two more (`codequery/search_authz_test.go`, `repository/authz_test.go`)
match under the same sed plus one gofumpt import-line resort each
(`query/selector` sorts after `query/querytestutil`, while
`query/queryselector` sorted before it). The remaining 11 importer
files (including `repository/selectors.go`, both `repository_selector.go`
families, `contentread/content_handler.go`, `iac/handler.go`, and five
`supply/chain` scopes) carry the extra `selector` → `rawSelector`
parameter rename; every changed line in their diffs contains the
`selector` or `rawSelector` token, so no other byte changed.

`scripts/verify-performance-evidence.sh` reports no hot
Cypher/concurrency/runtime files changed, so no benchmark applies: the
Cypher text inside the moved files is untouched and the call graph is
identical up to the qualifier rename. The two `query-source-coverage.yaml`
rows for the moved files rename only the `file:` key (`queryselector/` →
`selector/`); their `source_sha256` pins hash the extracted symbol source,
which excludes the package clause, so they stay valid. One hot-callsite pin
outside the moved files IS touched: `entity/handler.go:(*Handler).ResolveEntity`
calls through the renamed qualifier twice inside the function body, so its
symbol-source digest moves `91a04b4e…` → `07158242…`. The refresh is
qualifier-only (the function diff shows exactly two `queryselector.` →
`selector.` token changes and nothing else), the new digest was recomputed
independently with `go/ast` extraction plus sha256 (matching the
`manifestSymbolSource`/`sourceNodeText` method), and the full
`go test ./internal/queryplan/ -count=1` plus
`scripts/verify-query-plan-regression.sh` pass with no other pin drifting. `scripts/verify-moved-file-refs.sh` reports 4 vacated
Go paths against base `9cf27f072` with no dangling references (dated
evidence and design docs that name the old path stay pinned via four
new `scripts/moved-file-refs-allowlist.txt` rows), and
`scripts/verify-package-docs.sh` reports the moved doc trio present.
`go build ./...` (exit 0) plus `go test ./internal/query/... -count=1`
(all ok, no failures) prove the repointed call graph still resolves and
behaves.

No-Observability-Change: no span name, attribute, metric, or log line
changes; the move touches no telemetry emission point.

## Performance and observability evidence for the `deployment` leaf

No-Regression Evidence: 50 files move from `query/impacttrace/` to
`query/impact/deployment/` (48 Go files including `doc.go`, plus
`README.md`/`AGENTS.md`), package clause `impacttrace` to `deployment`,
and 61
importer files repoint the import path and the `impacttrace.` qualifier
to `deployment.`. File names drop the `impact_trace_`/`deployment_trace_`
prefixes per the #6818 plan, plus the leading `deployment_` the
filename-stutter gate requires inside a `deployment/` leaf (rule 2:
the stem must not repeat the leaf directory name, so
`impact_trace_deployment_k8s.go` becomes `k8s.go`, not
`deployment_k8s.go`). No call site, argument, Cypher text, allocation,
loop bound, span, metric, or log change.

Before tree `652fe235c5985d1e84cf4f0d9d306ee7f1a436a6`
(`a12cdb2c:go/internal/query/impacttrace`), after tree
`e17251df7f301a7356dd2f0d58b9237afa883367`
(`HEAD:go/internal/query/impact/deployment`).

Moved-file substitution proof, base `a12cdb2c`, run from the worktree
root with the old→new map (`impact_trace_`/`deployment_trace_` stripped,
then a leading `deployment_` stripped; 48 pairs, no collisions):

```bash
while read line; do
  old="${line% -> *}"; new="${line#* -> }"
  git show "a12cdb2c:go/internal/query/impacttrace/$old" \
    | sed 's/impacttrace/deployment/g' \
    | cmp -s - "go/internal/query/impact/deployment/$new" && echo "CLEAN $old"
done < /tmp/impact-rename-map2.txt
```

48/48 moved files are byte-exact under the single sed plus the rename;
the content transform is the package clause, qualifier-free prose, and
one test-only meter name (`impacttrace-scoped-grant-test` →
`deployment-scoped-grant-test`, asserted nowhere).

Importer substitution proof (same base; the second sed expression repairs
only the import path, which the first sed shortens):

```bash
while read f; do
  git show "a12cdb2c:$f" \
    | sed 's/impacttrace/deployment/g; s|query/deployment"|query/impact/deployment"|' \
    | cmp -s - "$f" && echo "CLEAN $f"
done < /tmp/impact-importers.txt
```

59/61 importer files are byte-exact. The two exceptions are each one
known extra: `impact/trace_deployment_k8s_select_widening_test.go`
renames seven shadowing `deployment` locals to `k8sDeployment` (a K8s
Deployment entity map; `deployment.BuildK8sRelationships` qualifier
sites untouched), and `impact/deployment_config_influence.go` adds the
`//nolint:dirgate` package-clause marker the new subpackage forces
(`deployment_config_influence{,_limits}.go` declare `*Handler` methods,
which `deployment/` forbids by its AGENTS.md contract, so they stay in
`impact` with the gate-sanctioned marker citing #6818).

`scripts/verify-performance-evidence.sh` passes (exit 0). Five
importer-side `query-source-coverage.yaml` pins refresh, all
qualifier-in-body class, each body diff proven token-mechanical and each
digest independently recomputed with `go/ast` extraction plus sha256:

- `impact/contract.go:(*Handler).contractImpactResponse`:
  `e1bf16c4…` → `674df629…`
- `impact/handler.go:(*Handler).traceResourceToCode`:
  `281827d1…` → `728855a8…`
- `impact/resource_investigation_reads.go:(*Handler).ResourceInvestigationRepoPaths`:
  `28e0fdff…` → `64187a07…`
- `impact/resource_investigation_reads.go:(*Handler).ResourceInvestigationWorkloads`:
  `0c266cfc…` → `2056d923…`
- `impact/trace_deployment_resources.go:(*Handler).fetchCloudResourceResult`:
  `9dd90cb7…` → `8a8ae375…`

Six `file:` keys repoint (`impacttrace/` → `impact/deployment/` with the
new basenames); their pins hash extracted symbol source and stay valid.
`hot-cypher.yaml` repoints three rows, `specs/live-tests.v1.yaml` two
rows, `specs/language-feature-parity-ledger.v1.yaml` one row. The
B-12 snapshot description naming the renamed live-evidence test is
repointed (provenance prose only; no asserted payload changes, JSON
still parses, `go test ./cmd/golden-corpus-gate/` passes).
`docs/public/observability/telemetry-coverage.md` repoints its
`workload_selection.go` row; `scripts/verify-telemetry-coverage.sh`
passes. Three public doc test-path pointers repoint; five dated
evidence/design docs keep BEFORE-state paths via new
`scripts/moved-file-refs-allowlist.txt` rows.
`scripts/verify-moved-file-refs.sh` reports 48 vacated Go paths with no
dangling references; `scripts/verify-package-docs.sh` reports the moved
doc trio present. `go build ./...` and `go vet ./...` exit 0;
`go test ./internal/query/impact/deployment/ -count=1` passes (86
tests, `go test -list` confirms discovery);
`go test ./internal/query/... ./internal/mcp/... ./internal/queryplan/ -count=1`
pass with no failures. The Cypher text inside the moved files is
untouched and the call graph is identical up to the qualifier rename,
so no benchmark applies.

No-Observability-Change: no span name, attribute, registered metric, or
log line changes; the one meter rename is test-only instrumentation.
`TestResolveWorkloadSelectorOperationLabel` still passes: the emitted
`deployment_trace_selector` operation literal is untouched.

## Performance and observability evidence for the `session` leaf (move 4a)

No-Regression Evidence: 4 files move from `query/queryauth/` to
`query/auth/session/` (new intermediate dir), package clause `queryauth`
to `session`: `browser_session_types.go` → `browser_types.go`,
`session_cookies.go` → `cookies.go` (+ test), `session_timeouts.go` →
`timeouts.go` (the `session_` prefix strip the filename-stutter gate
requires inside a `session/` leaf; `browser_session_types.go` drops the
middle `session` word for the same rule). `queryauth` keeps `AuthContext`,
`AuthMode`, the context key, and the sign-in policy shape; the moved
files name them `queryauth.`-qualified (6 identifiers:
`AuthContext`, `AuthMode`, `AuthModeScoped`, `AuthModeBrowserSession`,
`NormalizeAuthContext`, `SignInPolicyReadStore`), so `session` imports
`queryauth` and no cycle exists (proven: no staying file references a
moved symbol). 8 external files repoint `queryauth.Session*` qualifiers
to `session.` (a ninth, `local_identity_alias.go`, needed only a
comment-qualifier touch); root `query` keeps its aliases/forwarders
spelling `session.` names. No call site, argument, or behavior change.

Moved-file blob pairs (before → after):

- `browser_session_types.go` `f0507f7a` → `browser_types.go`
  `3712c8fb` (package clause, queryauth import add, 10 qualifier
  occurrences on 9 lines incl. 2 in doc comments, plus one gofumpt
  import resort)
- `session_cookies.go` `3e8abb9f` → `cookies.go` `01e540aa`
  (package clause only)
- `session_cookies_test.go` `931c80dd` → `cookies_test.go`
  `3f9c12ab` (package clause only)
- `session_timeouts.go` `50d7cf05` → `timeouts.go` `59bab1eb`
  (package clause, queryauth import add, 1 qualification site)

Stale home prose (`lives in queryauth`) repointed to `session` in the
touched alias/forwarder files only where the named symbol moved;
`AuthContext`, `GovernanceAuditAppender`, and `SignInPolicyReadStore`
comments correctly still name `queryauth`. New `doc.go`/`README.md`/
`AGENTS.md` trio for `session` carries the cookie-pairing (#4964),
timeout (#4968), single-key, and no-querycontract-cycle invariants;
`queryauth/doc.go` records the relocation.

`scripts/verify-performance-evidence.sh` passes (exit 0): no
Cypher/concurrency/runtime change, so no benchmark applies — the moved
bodies are identical up to the qualifier, and the call graph is
unchanged. No `query-source-coverage.yaml`, `hot-cypher.yaml`,
live-tests spec, parity-ledger, telemetry-coverage, golden-snapshot, or
public-doc row names a moved path (swept: zero hits), and no dated
evidence/design doc does either, so no pin refresh and no allowlist row.
`go build ./...` and `go vet ./...` exit 0;
`go test ./internal/query/... ./internal/oidcbearer ./internal/scopedtoken -count=1`
passes with no failures (19 run-lines in `session`, real case counts);
`go test ./internal/queryplan/ -count=1` passes with no pin drift;
`go test -list '.*' ./internal/query/auth/session/` discovers the moved
cookie test. `scripts/verify-moved-file-refs.sh` reports 4 vacated Go
paths against base `aaa5273a` with no dangling references (no dated doc
names a moved path, so no allowlist row);
`scripts/verify-package-docs.sh` reports the new `session` doc trio
present; `scripts/verify-performance-evidence.sh` passes (exit 0).

No-Observability-Change: no span name, attribute, registered metric, or
log line changes; the move touches no telemetry emission point.

## Performance and observability evidence for the `auth` rename (move 4b)

No-Regression Evidence: 8 files move from `query/queryauth/` to the
existing `query/auth/` dir (created by 4a for `session/`), package clause
`queryauth` to `auth`, no file renames: `context.go`, `audit_actor.go`,
`permission_catalog.go`, `sign_in_policy.go`, `doc.go`,
`context_normalize_test.go`, `README.md`, `AGENTS.md`. The moved bodies
are identical up to the clause line and doc prose (no qualifier or logic
change — the moved package names its own symbols bare). 114 files
repoint the import `query/queryauth"` to `query/auth"` and the qualifier
`queryauth.` to `auth.`; root `query` keeps its `AuthContext`/`AuthMode`
aliases and forwarders, now spelling `auth.` names, so external callers
(`oidcbearer`, `scopedtoken`, `ask/engine`) are unaffected. 39 shadowing
`auth` locals/params across 37 files renamed to `authCtx` (the
established convention, already used in `repository` tests) where a
package-level symbol (`NormalizeAuthContext`, `AuthModeShared`,
`PermissionFeatureTokens`, `ContextWithAuthContext`,
`ActorClassForAuth`, composite `auth.AuthContext{}`) is named after the
shadow; field-only uses needed no change. `session` repoints its parent
import (`queryauth.AuthContext` to `auth.AuthContext`); the edge stays
one-directional `session` → `auth`, and `auth` still does not import
`querycontract`.

Moved-file blob pairs (before → after):

- `context.go` `dcb3d3f7` → `927377ea` (package clause only)
- `audit_actor.go` `ecb18678` → `df5abc83` (package clause only)
- `permission_catalog.go` `d48e4e1d` → `6d08ec4c` (package clause only)
- `sign_in_policy.go` `f083af57` → `16002dc2` (package clause only)
- `doc.go` `3d3cc5ab` → `b988d345` (package clause + `Package auth owns`)
- `context_normalize_test.go` `9a008d3d` → `ddb3ea6e` (package clause only)

Pin check (2 rows, `go/internal/queryplan/testdata/handler-hot-cypher.yaml`,
QP-CALL-GRAPH-HUBS and QP-CALL-GRAPH-RECURSIVE): no refresh needed.
`codequery/metrics/edges.go` is byte-identical to base, so the pinned
`source_sha256` (`a371b5f4…`) for `CallGraphMetricsEdgesCypher` still
matches and `TestHandlerQueryplanManifestBindsProductionBuilders`
passes. (A mid-move `gofmt` run with the host Go 1.27.1 toolchain
briefly reflowed that file's alignment and tripped the test; the
commit hook's pinned `golangci-lint fmt` restored the canonical bytes
and the rows were left untouched. Lesson recorded: never bare `gofmt`
in a Go 1.26.6-pinned tree — the hook formatter is the authority.)

Stale home prose (`lives in queryauth`, `Package queryauth`, leaf lists)
repointed to `auth` across the touched handler-family `doc.go` files,
consumer `README.md`/`AGENTS.md`, `auth/session` docs, and
`source-layout.md`. Untouched: rule-illustration mentions (`naming.md`,
`verify-filename-stutter.sh`, `queryplan/AGENTS.md` #6060 narrative),
#6642 design docs, older evidence sections, and `moved from queryauth
by #6818` provenance lines. No `query-source-coverage.yaml`,
live-tests spec, parity-ledger, telemetry-coverage, golden-snapshot, or
CI path-filter row names a moved path (swept: zero hits), and no dated
evidence/design doc does either, so no allowlist row.

`go build ./...` and `go vet ./...` exit 0;
`go test ./internal/query/... ./internal/oidcbearer ./internal/scopedtoken -count=1`
passes with no failures (60 ok lines);
`TestHandlerQueryplanManifestBindsProductionBuilders` passes after the
pin refresh. Test-name union (`auth/session` + `auth` + `querycontract`
+ root `query`, `-list '.*'`) is 2727 names, byte-identical to the
pre-move (`auth/session` + `queryauth` + `querycontract` + root) union:
nothing dropped, added, or renamed.
Creating `query/auth/` trips the dirgate sibling rule for the 32
`auth*.go` files staying in root `query` (aliases, forwarders, route
policies, handler wiring); each carries
`//nolint:dirgate // #6818 move 4b: …` on its package line, following
the established root-shim precedent — relocating root's auth surface
is a separate follow-up, not this rename. Whole-tree
`verify-dirgate.sh --all` passes.
`scripts/verify-moved-file-refs.sh` reports the 6 vacated Go paths with
no dangling references; `scripts/verify-package-docs.sh` reports the
`auth` doc trio present; `scripts/verify-performance-evidence.sh`
passes (exit 0).

No-Observability-Change: no span name, attribute, registered metric, or
log line changes; the move touches no telemetry emission point.

## Performance and observability evidence for the `testutil` split (move 5)

No-Regression Evidence: two commits on `refactor/6818-testutil`. 5a
(`bc7134a73`) peels the fake `database/sql` driver family out of
`querytestutil/content` into the new `internal/testutil/contentreader`
leaf, package clause `contentreader`: `reader_args.go` to `args.go`,
`reader_columns.go` to `columns.go`, `reader_defaults.go` to
`defaults.go`, `reader_driver.go` to `driver.go`, plus the 4 reader
tests (names kept). The 4 non-test files are byte-identical up to the
package/import/selector swap (proven per file by normalized diff);
the tests additionally carry the `defaults.go` filename constant the
coverage self-check reads. 20 root test files repoint the import and
the `content.(Reader|OpenReader)` selectors; store-fake users keep the
content import. The peel also carries the `naming-glue-gate`
exemption feature below. 5b (`d93f8d614`) is the pure rename of the
56-file helper tree (parent + `content` and `graph` leaves) to
`query/testutil`, clause `testutil`: 2752+/2752- symmetric, 432
modified `.go` files in which every changed line is the identifier
swap — 394 with the swap in place, 38 with the renamed import
additionally resorted within its block by `gofumpt` (import-block
comparison; an independent recount classified 43 as resorted under a
stricter same-line rule — both agree zero files carry any other
change).

Byte-identity proof (all commands run against base `4e1e534b4`, exit
0): every 5b rename pair diffs empty after normalizing
`querytestutil` to `testutil`, except 4 doc files whose extra prose
documents the peel-out; every modified `.go` file diffs empty after
pairing off rename lines, except the 20 peel consumers and the 2 gate
files; the 8 peel files diff empty after normalizing the package,
import, and selector swap. No Cypher literal, queue bound, worker
count, or retry knob changes anywhere in the diff.

`queryplan`'s test-only-helper gate follows the rename: constant
`testOnlyHelperPackage` to `"testutil"`, fixtures and comments
repointed. The match stays a whole-path-element match, so a query
production file reaching `query/testutil`, its leaves, or
`internal/testutil/contentreader` still fails; the exemption still
covers only files inside `queryDir/testutil`. Zero non-test files
under `internal/query` import any of the three packages (swept), and
no `query-source-coverage.yaml` row names a moved path (swept: zero
hits), so no pin refresh. The name `testutil/contentreader` keeps the
owner-approved plan-table target: nesting as `content/reader` would
force clause `reader` and stutter `reader.Reader*` at every call
site (naming.md rule 4), with zero collisions measured for
`contentreader`; `scripts/lib/naming-glue-exempt.tsv` records that
decision and the glue gate filters the full path before
classification (a malformed ledger fails closed, exit 2).

`go build ./...` and `go vet` on the touched trees exit 0;
`go test -count=1` passes for root `query`, `queryplan`,
`query/testutil/...`, and `testutil/contentreader`
(`TestDiscoverQueryCallsites*` green, including the nested-leaf and
helper-imports-own-leaf fixtures). Test-name union: `go test -list`
under `./internal/query/...` registers 5172 Tests before and 5155
after; the 17 relocated Tests register under
`./internal/testutil/contentreader/` with byte-identical names
(proven by diff), so nothing dropped, added, or renamed.
`scripts/verify-moved-file-refs.sh --base 4e1e534b4` reports 58
vacated Go paths with no dangling references;
`scripts/verify-package-docs.sh` reports the `contentreader` doc
trio present; `scripts/verify-performance-evidence.sh` passes
(exit 0) on this section.

No-Observability-Change: no span name, attribute, registered metric,
or log line changes; the two commits touch no telemetry emission
point.
