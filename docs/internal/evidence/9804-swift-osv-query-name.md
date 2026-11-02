# Swift OSV query name: send `host/path`, not the clone URL (#9804)

## What changed at runtime

The vulnerability planner named a Swift OSV query with the dependency's clone URL
(`https://github.com/apple/swift-nio.git`). OSV's `SwiftURL` names are
`host/path` (`github.com/apple/swift-nio`), so the query matched nothing and
the miss was silent. `packageidentity.SwiftOSVSourceLocation` now returns the
OSV spelling, and the planner and the owned-package resolver (#9777) both keep
calling it.

The name is built like this:

- Scheme (`https`, `http`, `ssh`, `git`), userinfo, port, query, fragment,
  trailing slashes and a `.git` suffix (any case) are removed. `git@host:path`
  maps to `host/path`.
- The host is folded to ASCII lower case, as DNS does. The path keeps its case,
  because OSV matches the name byte for byte (the probe below shows it).
- `SwiftRepositoryKey` and `SwiftOSVSourceLocation` now sit on one parser,
  `swiftRepositoryParts`. The key is the ASCII fold of the same host and path,
  so the two cannot drift. `TestSwiftOSVSourceLocationAgreesWithTheRepositoryKey`
  pins that the key of the query name equals the key of the stored location.
- A location with no `://` and no `git@` prefix is still not queried (the
  existing `TestVulnerabilityIntelligenceWorkPlannerSkipsSwiftWithoutSourceURL`
  rule). A dotless host (`https://localhost/a/b`, an IPv6 literal) now also
  names no query, because no OSV name can have one and the repository key
  refuses it. Both report the existing `missing_source_location` skip reason.

Files: `go/internal/packageidentity/swift_repository.go` (plus tests),
`go/internal/coordinator/vulnerability/intelligence_derivation_swift*.go` and
tests, `go/internal/storage/postgres/owned_package_resolver_{fixture,live}_test.go`
(fixture keys now use the planner's new names), and
`go/internal/collector/vulnerabilityintelligence/vulnruntime/source_derived_swift_test.go`.
No SQL, schema, payload or fact shape changes.

## OSV probe

Read-only `POST https://api.osv.dev/v1/querybatch`, ecosystem `SwiftURL`, run on
1 November 2026. Each cell is the number of vulnerabilities returned for that
name at that version.

| Package @ version | `https://…git` (old) | `https://…` | `git@github.com:…git` (old) | `host/path` (new) | `host/path`, path lower-cased |
|---|---|---|---|---|---|
| apple/swift-nio @ 2.0.0 | 0 | 0 | 0 | **5** | 5 |
| apple/swift-nio-ssl @ 2.0.0 | 0 | 0 | 0 | **1** | 1 |
| apple/swift-nio-http2 @ 1.0.0 | 0 | 0 | 0 | **7** | 7 |
| vapor/vapor @ 4.0.0 | 0 | 0 | 0 | **6** | 6 |
| grpc/grpc-swift @ 1.0.0 | 0 | 0 | 0 | **4** | 4 |
| swift-server/async-http-client @ 1.0.0 | 0 | 0 | 0 | **1** | 1 |
| mongodb/mongo-swift-driver @ 1.0.0 | 0 | 0 | 0 | **1** | 1 |
| marmelroy/Zip @ 1.0.0 | 0 | 0 | 0 | **1** | 0 |
| sparkle-project/Sparkle @ 1.0.0 | 0 | 0 | 0 | **3** | 0 |

Nine packages, 45 queries in one batch. Every old form returned 0; the new form
returned 1 to 7. The last column is why the path keeps its case: lower-casing
`Zip` and `Sparkle` loses their advisories. The consequence is a known limit
that predates this change: a manifest that spells the repository in the wrong
case (`github.com/marmelroy/zip`) queries a name OSV does not hold. The resolver
still joins it, because the key folds.

## Existing state under the old name

The query name appears in two persisted places, and this change moves both for
Swift only.

1. **Scope IDs.** With `slot_scopes` off (the default), a single-package scope
   is `vuln-intel://osv/swift/<url-escaped name>?version=…` and a batch scope
   lists `name@version`. A Swift scope therefore gets a new ID. The old scope
   (`…/swift/https:%2F%2Fgithub.com…`) is no longer planned. Nothing retires it:
   it is the same orphan class `intelligence_derivation.go` already documents for
   a chunk-count shrink (#9431). With `slot_scopes` on, the scope ID is
   `…/_slot/<virtual slot>/<chunk>` and depends on the reader's key hash, not
   on the name, so the scope keeps its ID and the next generation supersedes the
   old one at ack.
2. **Source-state package keys.** The collector records
   `packageidentity.ConsumptionKeys("swift", <query name>)` for a scope. The
   old rows hold the lower-cased clone URL. The readiness join compares them with
   the manifest dependency name, which the parser writes as `host/path` (the
   resolver tests build their swift rows through the real parser,
   `resolverParsedSwiftRows`), so the old keys never matched any dependency.
   They carry no readiness truth today. The new key
   (`github.com/apple/swift-nio`) is the same string the manifest side carries;
   `TestDerivedSwiftScopeRecordsTheOSVNameAndJoinsTheManifestName` pins it.

Every old Swift query also returned 0 vulnerabilities, so no advisory fact
exists under the old name to orphan. The QA environment has no Swift rows
today, so nothing is orphaned there either. No migration, rotation job or
retirement pass was built. Open question for the owner: if a deployment with
`slot_scopes` off already holds Swift scopes, should the orphaned old-name
scopes and their source-state rows be retired? The change does not need it, and
the orphans are inert.

A package already planned under the old name is not queried twice. The planner
dedups on `ecosystem + name + version` and the name is now the shared one, so
`https://…swift-nio.git`, `git@github.com:apple/swift-nio.git`,
`ssh://git@github.com/apple/swift-nio` and `https://…/swift-nio/` at one
version plan one query (`TestSwiftSpellingsOfOneRepositoryPlanOneQuery`). Before
this change they were four queries and, in batch mode, four scope entries.

## Edge cases

| Input | Result |
|---|---|
| `https`, `http`, `ssh://`, `git://`, `git@host:path`, trailing `.git`, `.GIT`, `.git/` | `host/path` with the suffix gone |
| Userinfo, `?query`, `#fragment` | removed (also keeps secrets out of the scope ID) |
| Port (`github.com:443`, `ssh://…:2222`) | removed; OSV names carry none |
| Upper-case host (`GitHub.COM`) | host folded, path case kept |
| `www.` prefix | kept, not folded; `SwiftRepositoryKey` does not fold it either, so both sides stay equal |
| Empty, blank, `git@host`, `git@host:`, `https://host/`, `https://host/.git` | `""`, skipped as `missing_source_location` |
| `file://`, `ftp://`, bare `swift-nio`, `apple/swift-nio`, scheme-less `github.com/a/b` | `""` |
| Dotless host (`localhost`), IPv6 literal | `""` |

## Telemetry

No-Observability-Change: the change picks a different string for an existing
query, so no new operator-visible behavior appears. The existing signals already
cover it: the derived-target skip counters carry `missing_source_location`,
which now also counts dotless-host locations, and the OSV collector's per-scope
request and result counters and the `owned_package_targets.resolve` span
(#9777) are unchanged. An operator who sees a Swift scope return zero results
after this change can read the query name in the scope ID.

## Scale

No-Regression Evidence: Swift rows on QA are 0 today, so the measured query
count there is zero before and after. The cost model does not depend on row
count: the planner makes one pass over the owned targets and builds the name
once per row with string operations (a parse and two folds, no I/O), the same
order of work as the old name. The OSV cost is per distinct
`ecosystem + name + version`, not per row. Because several spellings of one
repository now collapse to one name, the query count stays at or below the old
count at every size.

| Size | Swift rows | Distinct repositories (assumed) | Queries before | Queries after |
|---|---|---|---|---|
| 1x (QA today) | 0 | 0 | 0 | 0 |
| 5x of a mid fleet (illustrative) | 500 | 150 | up to 500 (one per spelling) | 150 |
| 10x | 1,000 | 300 | up to 1,000 | 300 |

The 5x and 10x rows are bounds from the dedup rule, not measurements. The
per-row cost is measured (a Go benchmark on the review host): 244 ns/op and 3
allocations for an https location and 115 ns/op and 2 allocations for the
`git@host:path` form in `SwiftOSVSourceLocation`, and 61 ns/op for
`SwiftRepositoryKey`. A row is parsed up to three times (the planner parts, the
key re-check and the resolver key), about 0.4 microseconds, so 1,000 rows cost
about 0.4 ms per plan. The planner's `target_limit` still caps queries per plan, and the scope sizer still
caps a batch scope.

## Verification

Commands ran with `env -u GOROOT GOWORK=off GOTOOLCHAIN=go1.26.10` in
the feature worktree after the last code edit.

- RED before the change: `go test -count=1 ./internal/packageidentity/ -run SwiftOSV`
  failed on every scheme, `.git`, port and `git@` case (for example
  `SwiftOSVSourceLocation("git@github.com:apple/swift-log.git") =
  "git@github.com:apple/swift-log.git", want "github.com/apple/swift-log"`).
- `gofumpt -l` on the touched files: no output, exit 0.
- `go vet` on `packageidentity`, `coordinator/vulnerability`,
  `vulnruntime` and `storage/postgres`: exit 0.
- `go test -count=1 ./internal/packageidentity/ ./internal/coordinator/vulnerability/
  ./internal/collector/vulnerabilityintelligence/...`: exit 0.
- Resolver, including the six live tests, on a private PostgreSQL 18 with
  `ESHU_POSTGRES_DISPOSABLE=1`: `go test -count=1 ./internal/storage/postgres/
  -run 'ResolveOwnedPackageNames|OwnedResolver|OwnedNameLookup|MatchOwned'`,
  exit 0. The live fixture's planner-name keys changed to the new names; the
  differential, feed, 33-name, rules, chunk and span tests all pass.
- Review fix (F1): the live fixture now holds two swift rows the planner never
  queries (`https://localhost/a/b` and a scheme-less `github.com/x/y`), and the
  rules test asserts that lookups for `localhost/a/b` and `github.com/x/y`
  resolve no target. Mutant: in `ownedResolverOSVName`, replace
  `packageidentity.SwiftOSVSourceLocation(row.sourceLocation)` with
  `strings.TrimSpace(row.sourceLocation)`. Before the assertion the six live
  tests stayed green with it; with it, `FollowsThePlannerRulesLive` fails (both
  lookups return a target) and exit 1. The file was restored byte for byte.
  Also pinned: `file://host.example/a/b` names no query (mutant: add `file` to
  the allowed schemes, killed), and a scp host with a space or tab names no
  repository key (mutant: drop the final whitespace check, killed by
  `TestSwiftRepositoryKeyRejectsWhitespaceInAScpHost`). The planner name check
  alone hides the second one. The `SwiftOSVSourceLocation` doc now names the two inputs where the
  key of the name differs from the key of the stored location.
- The wider `./internal/storage/postgres/` package has one failure unrelated to
  this change: `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline` needs
  an `ESHU_DEPLOY_CHECKOUT` clone of `eshu-deploy`.
