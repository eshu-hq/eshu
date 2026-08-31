# Package restructure: from flat thousand-file directories to a tree a human can read

Status: proposed. Research: 8-agent measured sweep, 2026-08-11 (per-package
family inventories, symbol-level dependency measurement, gate blast surface).
Tracked as epic #6053; this document is its committed plan.

## The problem, in numbers

| Package | .go files, one flat dir | Non-test / test |
|---|---|---|
| `internal/query` | 1,903 | 877 / 1,026 |
| `internal/reducer` | 1,269 | 536 / 733 |
| `internal/mcp` | 338 | 130 / 208 |
| `internal/parser` (root) | 259 | 47 / 212 |
| `internal/collector` (root) | 250 | 111 / 139 |
| `cmd/eshu` | 233 | 121 / 112 |
| `internal/projector` | 188 | 92 / 96 |
| `internal/coordinator` | 124 | 66 / 58 |

Three splits are corrected from the research, all counted on disk when this
was committed with `rg --files -g '*.go' --max-depth 1` against each
directory and the `*_test.go` subset of the same list. Collector read
`~240 / ~10+3`, which is the package-clause split (247 files say `package
collector`, 3 say `package collector_test`), not the filename split: it is
111 / 139. Projector and coordinator read `94 / 94` and `62 / 62`, which
the appendix marks "approx" and this table had dropped the hedge on; they
are 92 / 96 and 66 / 58. All three totals were already right.

The other rows are the research's own count and have drifted a little since
2026-08-11, which is the point the appendix makes about re-running the
census before any move: query now reads 1,910 files (879 / 1,031), reducer
1,270 (536 / 734), and mcp 340 (130 / 210). Parser and cmd/eshu still match
exactly.

3,165 of the query+reducer files were added since 2026-07-01. The cause is
our own 500-line file cap: it splits files but says nothing about
directories, so every split adds flat files. A gate fixed file size and
created directory sprawl. The fix is the same move one level up: a
directory gate, then a measured migration.

Why this matters beyond taste: a contributor opening `internal/query` sees
1,903 files and concludes nobody curates this codebase. The counter to
"this looks machine-generated" is a tree a stranger can navigate. And the
directories we create become the module seams #4047/#4398 (the
package-extraction program) need — a family graded "clean" today is a
candidate repo tomorrow.

## Part 1: the gate (lands first, conflicts with nothing)

Model it on the existing 500-line cap, which has two implementations that
must stay in lockstep: the golangci-lint plugin
(`tools/golangci-lint-filelength/filelength.go`) that CI runs, and the bash
mirror in `scripts/dev/precommit-go.sh` (`filecap` / `filecap-all`) that
pre-commit and the local gate runner use. The directory gate follows the
same pattern:

- **Rule 1 — size:** max 40 non-test `.go` files per package directory
  (tests excluded; they pair with subjects). 40 keeps every
  already-healthy package green and catches sprawl early.
- **Rule 2 — naming:** a file whose name prefix matches a sibling
  subdirectory's package name belongs in that subdirectory (catches the
  "new file dodges the tree" regression).
- **Escape hatch:** same `//nolint:<gate>` convention on the package line
  with a written justification (27 files use this for filelength today).
- **Grandfather:** digest-pinned list of the current offenders (the #5335
  gate's pattern) so the gate lands green and the list only shrinks;
  editing a grandfathered directory's file count upward un-grandfathers it.
- **Registry entry** in `specs/ci-gates.v1.yaml` triggered on `go/**`
  (broad glob = immune to the two-layer registry/workflow drift trap).
- **BITES proof** required: seeded violation goes red naming the directory
  and the two legal exits; green on revert.

Three of the families Part 3 calls clean and moves early are themselves over
the cap, counted on disk when this was committed (non-test `.go` files at
depth 1): `reducer/supply_chain_impact` 63, `query/code` 85,
`query/supply_chain` 61. Moving any of them into one new directory would
create a directory that fails this gate the moment it exists. They land
pre-split into nested subdirectories in the same move PR — the shape the
collector plan already uses (`gitrepo/snapshot`, `gitrepo/selection`).
Grandfathering is for directories that exist today and never for one a move
PR creates, because the pinned list only shrinks. The rest of the early
movers clear the cap: `query/impact` 39, `collector/git_snapshot` 24,
`collector/git_selection` 21, `reducer/container_image_identity` 18,
`reducer/code_call_materialization` 25.

## Part 2: harden the gates BEFORE anything moves

The research found the scariest class isn't gates that break — it's gates
that **pass silently on nothing** after a move. Fix these first, as their
own PR, before any file moves:

1. Non-recursive `go test ./internal/query -run '<names>'` in at least six
   scripts (`verify-replay-coverage-gate.sh:51`,
   `verify-hosted-governance-proof.sh:58,60,96,98`,
   `verify-ask-eshu-local-proof.sh:192,229`,
   `verify-hosted-governance-remote-compose-proof.sh:61,92`,
   `verify-query-plan-profile.sh:53`, `verify-query-plan-regression.sh:9`)
   plus `specs/ci-gates.v1.yaml:2166` — a `-run` regex matching zero tests
   exits 0 ("no tests to run"). Same class in
   `mcp-schema-drift.yml:199` and `security_intelligence_release_gate.sh:277`
   for `./internal/mcp`. Change to `/...` where safe; where a `-run` pin
   must stay package-scoped, add a "matched at least N tests" assertion.
2. `scripts/verify-route-coverage.sh:112` uses `find -maxdepth 1` — a moved
   handler file silently drops out of route-coverage checking.
3. `go/internal/payloadusage/load.go:37-64` and `:95-113` resolve
   decode-seam files with a NON-recursive
   `filepath.Glob(dir/"factschema_decode*.go")`, so a seam file moved into a
   subdirectory drops out of the manifest gate without a word. The reducer
   glob does fail when it matches nothing at all; the projector, query,
   loader, relationships and replay globs deliberately accept an empty
   match while those families migrate. Neither case catches a PARTIAL move,
   which is the one a restructure produces. (The research cited
   `paths.go:99-143`, which documents this behavior in the `Paths` field
   comments rather than implementing it.)
4. Run `go/cmd/ci-gates validate --drift` (checkPathFilterCoverage) in the
   registry gate by default, not only on demand — it exists precisely to
   catch registry-vs-workflow filter drift, but only checks literal
   triggers and only when invoked.
5. The 10 literal single-file gate triggers (list in the research:
   `git_snapshot_entity_buckets.go`, `materialized_edge_families.go`,
   `shared_projection.go`, 3× `sql_relationship_*`, 3×
   `gcp_resource_materialization*`, `intent.go`, plus
   `canonical.go`/projector and `mcp_setup*`/cmd-eshu) each need updating in
   the same PR as their move — the hardening PR adds an existence check so
   a stale trigger fails loudly (the telemetry-coverage gate already does
   this with `path_target_exists`; copy it).

`docs/public/observability/telemetry-coverage.md` (473 rows citing exact
paths) will fail LOUDLY on moves — that's correct behavior; each move PR
rewrites its rows. The parser ledgers
(`language-feature-parity-ledger.v1.yaml`, `parser-backing-ledger.v1.yaml`)
and cmd/eshu's spec references (`backend-conformance.v1.yaml:98-101`,
`ci-gates.v1.yaml:1269-1271,1848`) are the same shape. `internal/mcp` has
~80 hardcoded `go test ./internal/mcp` references across five spec files.

## Part 3: target layouts (measured, per package)

Full family tables with counts and extraction grades live in the
[research appendix](restructure-research.md). Summary of what moves and
what stays:

**collector (250 flat root files):** one new `gitrepo/` umbrella package
absorbing the git-specific families — snapshot(64), selection(39),
docs(41), observability, submodule, workflow-image, tfstate-glue,
service-catalog-glue, codeowners-glue, refs, tracked, webhook, priority,
fair-dispatch. Root keeps the shared seam every collector kind uses:
`Service`/`Source`/`Committer` and the `claimed_service*` family (backs ~15
other collector kinds).
git_snapshot↔git_selection↔git_source is a measured 3-way production
import cycle — they move together into gitrepo, not into separate
packages, until a dependency-inversion refactor earns the split. Five glue
families need disambiguated names (gitsubmodule, gittfstate, …) because
same-named sibling packages already exist.

**Correction, landed with #6056.** This section originally also listed
`git_source_types.go` and `git_fact_builder*` as staying in the root. They
do not, and could not. `git_source_types.go` declares `RepositorySnapshot`,
whose fields reach `GitRef`, `TerraformStateCandidate`,
`FunctionSummarySnapshot`, `FunctionSourceSnapshot` and
`DataflowFunctionSnapshot`, and that data model is woven through
git_snapshot_* and git_selection_*. Pinning the file in the root and letting
the compiler pull back every declaration it transitively needed converged at
**103 of the 111** non-test root files — the documented seam would have moved
eight files and left the directory as it was. Both files moved into
`gitrepo`, which cuts the root to 19 files and drops its grandfather row
entirely.

Two consequences worth knowing before the remaining children:

- The leaf emitters could not be peeled on their own either. Every one of
  them needs the fact-stream writer and the content records that the fact
  stream also calls into, so a leaf-only move closes an import cycle. The
  shared half became `gitrepo/gitmodel`, and the leaves sit below it:
  `gitrepo -> leaf -> gitmodel`, one direction only.
- `gitrepo` itself stays over the 40-file cap at 66 and carries a
  grandfather row. The remaining overage is the snapshot/selection/source
  cycle, which needs the dependency inversion this epic deliberately did not
  bundle with a move. The ledger still improves: the root's row at 111 is
  gone and the replacement is 66, and dirgate's ratchet means any later
  extraction has to re-pin it lower.

No-Regression Evidence: the reducer factdecode hoist (#6061 PR2) is a
relocation of the decode-failure classification and per-fact quarantine
mechanism, with no logic change. The renames in decode_error.go are case-only,
so lowercasing both sides is an exact equivalence test and its moved bodies are
identical to the base commit under it (positive control: appending one line
turns the check red). The other two regions need more than that test and were
checked separately: quarantine_record.go additionally requalifies `Domain` to
`reducercontract.Domain` — an alias of the same type, declared at
`go/internal/reducer/intent.go:9`, so the signature is unchanged — and
quarantine_writer.go differs only by rewritten file-location comments. `git diff --name-only <base>..HEAD -- testdata/ specs/` is empty, so the
golden-corpus recordings and the end-to-end snapshot cannot move. The repo-wide
reducer test-function inventory is byte-identical across the move, 3232
functions on both sides, so no coverage was lost when the quarantine tests
relocated. Backend and version unchanged (NornicDB, default local profile);
input shape unchanged; terminal queue and row counts unchanged, because no
queue, lease, Cypher, or projection path is touched. Whole-module `go build ./...` and `go vet ./...` exit 0, and the reducer tree
(`reducer`, `contract`, `dsl`, `factdecode`, `payloadcore`, `tags`, `tfstate`)
plus `storage/postgres`, `query` and `projector` test green.

Codegen: this PR adds seven forwarders, and PR1 established that a forwarder
can cost a CALLER its inlinability, so the set difference was measured rather
than assumed. `go build -a -gcflags=-m ./internal/reducer` gives 1329
can-inline entries on the base and 1330 here. NO caller lost inlinability:
the four entries that leave the list are the moved symbols themselves
(`FactDecodeError.FailureClass`, `.Retryable`, `.Unwrap`,
`quarantineWriterFromContext`), now reported under factdecode. The root
forwarders are inlinable and their cross-package targets inline into them, so
the two-hop path collapses.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The quarantine counter
`eshu_dp_reducer_input_invalid_facts_total` is emitted by the same code at its
new path, and the two telemetry-coverage rows that named the moved files are
repointed rather than duplicated.

No-Regression Evidence: the reducer payloadcore hoist (#6061 PR1) is a symbol
eviction, not a logic change. Baseline and after are the same behavior by
construction and that is asserted, not assumed: all 28 moved function bodies are
character-identical to the merge base once every moved identifier the body
references is capitalized, not only the declared one (six bodies call or
reference a sibling that moved with them: PayloadOrderedStrings, PayloadBool,
SupplyChainWorkloadIDsFromPayload, OCIRepositoryID, SourceOrderKey and
PreferMaxSourceOrderKey; the remaining three of the 31 symbols are consts and
have no body) (checked mechanically against `git show origin/main:<file>` for each,
with a seeded-defect positive control — removing `PayloadStr`'s `"<nil>"` guard
turns the check RED naming that line, reverting turns it GREEN), and
`git diff --name-only origin/main..HEAD -- testdata/ specs/` is empty, so every
B-7 cassette and the B-12 snapshot
(`testdata/golden/e2e-20repo-snapshot.json`, sha256
`d6329f7bce08e71319262319fcf435bd4a6770167b081550df691b1a84e76802`) are
byte-identical. Backend and version are unchanged (NornicDB, default local
profile); input shape is unchanged; terminal queue and row counts are unchanged
because no queue, lease, Cypher, or projection path is touched. The diff is
definitions, import blocks, and a mechanical call-site rewrite: 88 call sites
across 31 root files change from `firstNonBlank(...)` to
`payloadcore.FirstNonBlank(...)`, plus a handful of other sites repointed at
payloadcore to keep their callers inlinable. These are behavior-preserving
token swaps, not unchanged lines — the distinction matters for anyone scoping
later regression proof from this record. Equivalence is established by the
body comparison above, not by the call sites being untouched.

Codegen moves in both directions, and the net is positive. Measured with
`go build -a -gcflags=-m ./internal/reducer` on both refs:

- Package-wide inlined call sites rise from 12291 on main to 13985 here, on
  go1.27.0. These counts are toolchain-sensitive — go1.26.6 gives 12231 and
  13925 on the same trees — so quote the toolchain with the number.
- 16 functions GAIN inlinability. The raw set difference is 27 when the probe
  covers `./internal/reducer/...` — the bare `./internal/reducer` above emits no
  payloadcore lines and yields 16. The other 11 are
  new exported payloadcore symbols (`PayloadBool`, `CopyPayload` and the rest)
  that did not exist on main at all, so they are new code rather than a codegen
  change. Only the 16 below were functions before this PR. Not the moved helpers: they are unchanged and
  most were never inlinable (`PayloadStr` costs 148, `ToStringSlice` 188,
  `FormatTally` 233, all against a budget of 80). What became inlinable is the
  16 one-line ROOT FORWARDERS that inherited the old lowercase names —
  `payloadStr`, `payloadString`, `semanticPayloadString`, `anyToString`,
  `compactStringSlice`, `formatTally`, `toStringSlice`, `sourceOrderKey` and
  others — each now a single `return payloadcore.X(...)` whose callee is too
  expensive to inline into it. Go's inline cost is computed over the function
  body; file and package size play no part.
- 3 functions lose inlinability: `cicdWorkflowImageIsInputOnly`,
  `indexSLSAProvenanceEvidence`, `packageRepositoryName`. Their bodies are
  byte-identical to the base; only their inline cost changed, because calling a
  forwarder instead of the original raises the CALLER past budget 80. None is on
  a per-row hot path. `cicdWorkflowImageIsInputOnly` runs once per workflow
  image over two passes (`ci_cd_run_correlation_workflow_image.go:128`, its sole
  call site). `indexSLSAProvenanceEvidence` runs once per decoded
  attestation.slsa_provenance envelope (`sbom_attestation_attachment_slsa_index.go:28`,
  its sole call site). `packageRepositoryName` runs once per dependency-manifest
  fact inside the per-envelope loop (`package_consumption_correlation.go:257`).
  None is the shared-projection retract path

  Four further symbols are inlined at fewer sites — `derefString` -3,
  `payloadBool` -2, `strings.HasPrefix` -3, `trimmedCICDPtr` -1. None of the
  four LOST inlinability: the lost set is exactly the three above plus the
  deleted `firstNonBlank`, so these are changes in how often a still-inlinable
  function was inlined, not new regressions. Three distinct mechanisms produce
  them, and only the first is a codegen change at all. A lost caller takes its
  callee's inlined site with it: `cicdWorkflowImageIsInputOnly` calls
  `trimmedCICDPtr` (-1), and `indexSLSAProvenanceEvidence` calls `derefString`.
  A call MOVED out of the measured package: `payloadcore/identity.go` contains
  exactly three `strings.HasPrefix` calls, which is the whole of that -3 — the
  probe measures `./internal/reducer`, so a call that relocated into the
  subpackage stops being counted, and nothing about it got slower. And a call
  site was deleted: repointing `rowUsesRefreshFence` at `payloadcore.PayloadBool`
  removed its one source call to `payloadBool`. That single deletion accounts
  for the whole -2, because `rowUsesRefreshFence` was itself inlined at one
  site, so `payloadBool` earned two inline reports from it — its own body and
  the inlined copy. Source call sites go 10 to 9; inline reports go 11 to 9. All four are attributed. `payloadBool` -2 is not a lost
  inline at all but this PR's own repoint: `rowUsesRefreshFence` is inlinable on
  BOTH refs, and the two vanished reports are its own body and its single
  inlined copy, which called `payloadBool` on main and call
  `payloadcore.PayloadBool` here. `derefString` -3 splits across two lost
  functions, not one — `indexSLSAProvenanceEvidence` carried two of them and the
  `cicdWorkflowImageIsInputOnly` inline chain the third.

  The probe-scope caveat matters for reading any of this: `-gcflags=-m` reports
  only the package named on the command line, so a call that moved into
  payloadcore leaves the count without anything getting slower. That is the
  whole of `strings.HasPrefix` -3, and the same effect shows up in
  `strings.TrimPrefix` -1, `slices.Sort` -1 and the `time` internals -1 each.

  Five more functions lost inlinability in an earlier revision and were FIXED
  rather than measured, by repointing them at payloadcore directly so the
  forwarder hop disappears: `rowUsesRefreshFence`, `payloadBoolPointer`,
  `collapsedObservabilityValue`, `crossplaneEntityMetadataString` and
  `mapStringValue`. `rowUsesRefreshFence` is the one that mattered — it runs
  once per row on the shared-projection retract path, and an aggregate inline
  count cannot establish no-regression for a per-row loop however favourable the
  total looks. It is inlinable again and inlining at its retract-path call site,
  verified with `-gcflags=-m`.

One forwarder does not exist for the same reason. `firstNonBlank` is inlinable
in the reducer root at cost 78; a forwarder around `payloadcore.FirstNonBlank`
costs 82, over Go's budget of 80, because the callee inlines into its own
forwarder. Keeping it would have dropped ALL 88 of its inlined call sites to 0.
Its 88 call sites across 29 non-test root files call the package directly instead, which
holds the count at 88 on both refs — parity with main, not an improvement.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The four new `payloadcore` files are
pure helpers that emit no signal; reducer execution stays covered by
`eshu_dp_queue_claim_duration_seconds`, `eshu_dp_reducer_queue_wait_seconds`
and `eshu_dp_queue_depth`, and shared projection by
`eshu_dp_shared_projection_cycles_total` and
`eshu_dp_shared_projection_step_seconds`, as the four rows added to
`docs/public/observability/telemetry-coverage.md` record.

No-Regression Evidence: the collector restructure is a file move. Baseline and
after measurement are the same tree by construction, and that is asserted rather
than assumed: `testdata/golden/e2e-20repo-snapshot.json` and every cassette
under `testdata/cassettes/` are byte-identical to the merge base (empty
`git diff`, snapshot sha256 `42e75ccf5a34f69d0f92a2e81cc53c0e281f70c37e4dc444d60dddeaf3c0826c`
on both sides, identical `git ls-tree` digests), so the B-12 contract the B-7
golden-corpus gate diffs against did not move. Backend and version are unchanged
(NornicDB, default local profile); input shape is unchanged (the same 20-repo
corpus); terminal queue and row counts are unchanged because no reducer,
projector, queue, lease, or Cypher path is touched — the diff is `git mv` plus
import-path and qualifier edits, with 211 git-detected renames and every
modified Go line a `collector.X` -> `gitrepo.X` swap. Whole-module `go build
./...` and `go vet ./...` exit 0 and the collector tree plus its `cmd/` callers
run green over 485 packages. A restructure that changed projected truth would
surface as a B-12 diff; there is none.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The `#nosec`, `//nolint:` and `//go:build`
annotation sets are identical before and after (538 `#nosec` with matching
per-code breakdown, 103 `//go:build`), and every telemetry-coverage row that
cited a moved file now cites its new path, with five new rows for the files this
change creates — four of them inert type/helper files, and
`gitrepo/gitmodel/factstream.go` mapped to `eshu_dp_workflow_claim_facts_emitted_total`
because its `Send` increments the counter feeding `CollectedGeneration.FactCount()`.
`scripts/verify-telemetry-coverage.sh` exits 0.

**projector (188) + coordinator (124):** projector's per-provider intent
families are measured clean (zero cross-family calls; all 44 calls fan out
from `scope_generation_intents.go` across 41 family files). Root keeps `canonical*`,
`runtime_*`, `stage_*`, decode helpers, payload readers, the dispatcher,
and failure/retry infra (~70 files). Hazard: canonical Row types are
consumed by 182 external files; family moves need qualifier updates or root
aliases, and the `canonical.go` exact-path gate trigger (#5531) moves in
lockstep. Azure, GCP, Kubernetes, EC2, S3, and security intent builders now use the
neutral `internal/projector/intent` boundary while root retains assembly,
lifecycle, enqueue, retry, and telemetry. EC2's `USES_PROFILE` builder is the
first extracted family to need a typed-payload decode; it keeps its own local
decode call against `sdk/go/factschema` rather than importing root's
classified decode wrapper, since importing root would create the same cycle
`ReducerIntent` and `FactLookup` route around. S3's LOGS_TO,
external-principal-grant, and internet-exposure intent builders moved into
`internal/projector/s3` the same way; its LOGS_TO builder is the second family
to need a typed-payload decode and follows the same local-decode pattern. All
three S3 builders share the generic `aws_resource_materialization:<scope>`
entity key rather than a family-distinct one, since they gate on the same AWS
`CloudResource` canonical-nodes phase the reducer publishes for every
AWS-provider scope. Coordinator `_scheduler.go` halves extract cleanly
(they implement a root Planner interface); the `_service.go` halves are
methods on the shared `Service` struct and stay until Service is
decomposed — a design decision, not a file move. Shared plan-key validation now
lives in dependency-neutral `internal/coordinator/plannercontract`. The CI/CD
run scheduler now demonstrates the first provider extraction under
`internal/coordinator/cicdrun`: the child owns its request and planner while
root keeps the structural interface, scheduling order, durable open-target
admission, retry, and telemetry. The provider security-alert scheduler is the
second extraction under `internal/coordinator/securityalert`: the child owns
its request and planner while root keeps the same scheduling, plan-key,
admission, retry, and telemetry responsibilities. The hosted SBOM-attestation
scheduler is the third extraction under `internal/coordinator/sbomattestation`
with the same boundary. The Vault metadata scheduler is the fourth extraction
under `internal/coordinator/vaultlive`; its pure planner moves while root keeps
scheduling, admission, retries, and telemetry. The Grafana Tempo scheduler is
the fifth extraction under `internal/coordinator/tempoplanner`; its
deterministic request validation, target filtering, and workflow-row
construction move while root keeps service scheduling, the plan-key clock,
tenant and egress filtering, durable admission, retries, and telemetry. The
Grafana Loki scheduler is the sixth extraction under
`internal/coordinator/lokiplanner`; its deterministic request validation,
target filtering, and workflow-row construction move under the same ownership
boundary. The scanner-worker scheduler is the seventh extraction under
`internal/coordinator/scannerworker`; the child owns configuration validation,
requested-scope privacy, configured target order, deterministic IDs, and
fairness-key construction. Root keeps the interface, scheduling and plan-key
clock, active and claims gates, tenant-grant and collector-egress gates, durable
admission, retries, queue and lease behavior, and telemetry. The
Prometheus/Mimir scheduler is the eighth extraction under
`internal/coordinator/prometheusmimir`; the child owns all five request fields,
enabled-target validation and filtering, configured order, deterministic IDs,
requested-scope privacy, trigger precedence, and per-target fairness keys. Root
keeps scheduling order, its plan-key clock, tenant and egress filtering,
empty-item admission skips, durable admission, retries, queue and lease
behavior, and telemetry. The Grafana scheduler is the ninth extraction under
`internal/coordinator/grafanaplanner`; the child owns all five request fields,
all-target validation before disabled and scope filtering, configured work-item
order, deterministic IDs, requested-scope privacy, trigger precedence, and the
target-instance-to-scope fairness fallback. Root keeps scheduling order, its
plan-key clock, collector-egress filtering, tenant-grant authorization,
empty-item admission skips, durable admission, retries, queue and lease
behavior, and telemetry. PagerDuty and Jira are the tenth and eleventh
extractions under `internal/coordinator/pagerdutyplanner` and
`internal/coordinator/jiraplanner`. Each child owns all five request fields,
all-target validation before scope filtering, webhook-scope membership,
configured order, deterministic IDs, privacy, and trigger precedence;
PagerDuty partitions fairness by provider and Jira by site. Root keeps
scheduling, clock, policy filtering, empty-item skips, durable admission,
freshness-trigger transitions, retries, queue and lease behavior, and
telemetry. GCP is the twelfth extraction under
`internal/coordinator/gcpplanner`; the child owns request validation,
scope-configuration parsing and defaulting, duplicate and field validation,
requested-scope filtering, requested-scope privacy, and deterministic
work-item construction. Unlike the other eleven, root's own freshness handoff
loop (`service_gcp_freshness.go`, which stays in root: it is a `_service.go`
half, a set of methods on the shared `Service` struct) needed the same scope
parsing the planner owns to match an inbound Cloud Asset Inventory
change-event trigger against configured scopes, and root's config loader
needed the same parsing to validate a claim-enabled GCP instance at startup.
Both call sites now go through two new child exports built for this
purpose — `EnabledScopes` (returning a privacy-scoped `ConfiguredScope`
without content_family or the credential handle) and
`ValidateClaimSchedulerConfiguration` — rather than reaching into the child's
private configuration types, following the same export-a-query-function
precedent `jiraplanner.HasConfiguredScope` and
`pagerdutyplanner.HasConfiguredScope` set for their own freshness call sites.
Root keeps scheduling order, the plan-key clock, tenant-grant authorization,
durable admission, freshness trigger claim/handoff/reap, retries, queue and
lease behavior, and telemetry. These moves do not change scheduler order,
workflow wire values, concurrency, or observability.
The generic component extension scheduler is the thirteenth extraction under
`internal/coordinator/componentextensionplanner`, and the first to hit the
acyclic-boundary problem Part 3's prerequisite section describes for query,
reducer, projector, and mcp. `parseComponentInstanceConfig` — the shared
generic component-activation configuration parse/validate function the
scheduler's core planning function returns and every one of its helper
functions consumes — was not scheduler-owned: `component_activation_config.go`
(root) constructs values of that type when it builds a collector instance's
`Configuration` JSON, and `pagerduty_service.go` and `governance_audit.go`
(root) also read it, for reasons unrelated to component-extension
scheduling. Because root already imports the planner package for the
request type, the planner package cannot import root back, so the type
could not stay in `component_activation_config.go`; and exporting it from
the planner would make two unrelated providers depend on a
scheduler-specific package, the same shape `owned_package_target_helpers.go`
and `target_priority.go` avoid by staying in root. The fix landed as its own
commit, before the scheduler moved: the type and its parser were hoisted
into a new dependency-neutral package, `internal/coordinator/componentactivation`
(`Config`, `RuntimeConfig`, `ParseConfig`) — the same
hoist-to-a-neutral-package pattern `internal/projector/intent` already uses
for the projector families' equivalent problem. `component_activation_config.go`,
`pagerduty_service.go`, `governance_audit.go`, and `componentextensionplanner`
all import `componentactivation`; none of them imports another from this
list, and `componentactivation` imports neither `coordinator` nor
`componentextensionplanner`. Root keeps scheduling order, hosted extension
egress-policy filtering and audit, durable admission, retries, queue and
lease behavior, and telemetry. These moves do not change scheduler order,
workflow wire values, concurrency, or observability.
Terraform-state keeps its separate plan-key validator, and the root
`firstNonBlank` helper remains outside this boundary.

**mcp (338):** two layers. Registration (`tools_<domain>.go`, 43
constructors, zero lateral calls) moves cleanly. Routing is the tangle:
`dispatch.go`'s 490-line switch inlines ~20 families' routing, and
`dispatch_repositories.go` is a hidden second router fanning out to 13
other families. Wave 1: the 11 families whose Route funcs are already
isolated. Wave 2: extract per-family Route funcs out of the two hub
switches, then move. The consumer-existence gates and ~35 package-wide
contract/authz test sweeps stay in root.

The documentation registration family is the first extracted MCP family. Its
six definitions live under `internal/mcp/documentation`, while the root keeps
both existing assembly positions, documentation routing, dispatch,
authorization, and transport ownership. The move uses the dependency-neutral
`internal/mcp/toolcontract` shape and does not combine the two constructor
groups or change the 162-tool order.

The cloud registration family is the second extracted MCP family. Its inventory
and runtime-drift definitions live under `internal/mcp/cloud`, while the root
keeps both assembly positions and all cloud routing, dispatch, authorization,
and transport ownership. The move uses `internal/mcp/toolcontract` and leaves
the 162-tool order unchanged.

The visualization family is the third MCP extraction. Its definition, family
membership, and pure `routecontract` request selection live under
`internal/mcp/visualization`. The root keeps its assembly position between
work-item and freshness tools plus global fanout, its private adapter, dispatch,
authorization, summaries, transport, and all operational behavior. The
162-tool order and HTTP request remain unchanged.

The Ask family is the fourth MCP extraction. Its definition, family membership,
and pure `routecontract` request selection live under `internal/mcp/ask`. The
root keeps its assembly position after reachability and before relationship
edges plus global fanout, its private adapter, dispatch, authorization,
summaries, transport, and all operational behavior. The 162-tool order and HTTP
request remain unchanged.

The query-playbook registration family is the fifth extracted MCP family. Its
two definitions live under `internal/mcp/playbooks`, while the root keeps their
assembly position after documentation tools and before investigation workflows
plus all query-playbook routing, dispatch, authorization, and transport
ownership. The move uses `internal/mcp/toolcontract` and leaves the 162-tool
order unchanged.

The relationship family is the sixth extracted MCP family. Its three
definitions live under `internal/mcp/relationships`: the code story and
analysis definitions remain at zero-based positions 8 and 9 in the codebase
group, and the relationship-edge definition remains after Ask and before
repository files. The same child package owns `CodeRoute` and `EdgeRoute`, pure
selectors that decide family membership and convert decoded arguments into
`internal/mcp/routecontract` requests. Root keeps ordered assembly, global
fanout order, thin route adapters, dispatch, authorization, transport,
timeouts, response budgets, envelopes, and telemetry. `internal/query` keeps
relationship validation, graph reads, bounds, and response shaping. The
extraction leaves the 33-tool codebase group and 162-tool global order
unchanged.

The freshness registration family is the seventh extracted MCP family. Its
four definitions live under `internal/mcp/freshness`, while the root keeps
their assembly position after visualization and before context tools. Routing
also stays in root: `get_repository_freshness` remains in
`dispatch_repositories.go`, and the other three definitions remain in
`dispatch_freshness.go`. The move uses `internal/mcp/toolcontract` and leaves
the 162-tool order unchanged.

The semantic registration family is the eighth extracted MCP family. Its three
definitions live under `internal/mcp/semantic`, while the root keeps the
semantic-evidence and semantic-search assembly positions after investigation
packets and before documentation finding aggregates. Routing also stays in
root: the evidence pair remains in `dispatch_semantic_evidence.go`, and search
remains in `dispatch_semantic_search.go`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The investigation registration family is the ninth extracted MCP family. Its
two workflow and three evidence-packet definitions live under
`internal/mcp/investigation`, while the root keeps both assembly positions
after query playbooks and before semantic evidence. Routing also stays in root:
workflow discovery and resolution remain in
`dispatch_investigation_workflows.go`, and the three packet exports remain in
`dispatch_investigation_packets.go`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The service registration family is the tenth extracted MCP family. Its catalog
definition, three service-context and investigation definitions, and
intelligence-report definition live under `internal/mcp/service`, while the
root keeps all three assembly positions. Routing also stays split in root:
catalog correlations remain in `dispatch_repositories.go` and
`dispatch_service_catalog.go`; service context, story, investigation, and
intelligence-report routes remain in `dispatch.go` and
`dispatch_service_selector.go`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The ecosystem registration family is the eleventh extracted MCP family. Its
23 definitions live under `internal/mcp/ecosystem`, while the root keeps their
single assembly position after repository-language tools and before
infrastructure aggregates. Routing stays split across the existing root
routers: ecosystem summaries and change planning remain in
`dispatch_ecosystem.go`; repository reads remain in
`dispatch_repositories.go`, and package-registry reads moved to
`internal/mcp/packageregistry` in the first Wave 2 extraction below;
infrastructure reads remain in `dispatch.go` and
`dispatch_infra_search.go`; impact reads remain in `dispatch_impact.go`; and
environment comparison remains in `compareRoute`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The package-registry route family is the first Wave 2 MCP extraction, and the
first that moves route selection without moving a registration. Its six tools
were answered by arms of the 46-arm `repositoryRoute` switch in
`dispatch_repositories.go`; family membership and pure `routecontract` request
selection now live under `internal/mcp/packageregistry`. Root keeps every tool
definition and its assembly position, global fanout order, the thin
`packageRegistryRoute` adapter, dispatch, authorization, transport, timeouts,
response budgets, envelopes, summaries, and telemetry. The adapter is consulted
at the top of `repositoryRoute` rather than as a new entry in `resolveRoute`, so
the repository router keeps its own position in the global chain and no other
family's resolution order changes. The six tool names are disjoint from the
remaining switch arms, and the 162-tool order, the advertised schemas, and every
selected method, path, and query key remain unchanged.

The CI/CD run-correlation route family is the second Wave 2 MCP extraction and
follows the same shape. Its three tools — the bounded run-correlation listing
and the two run-correlation aggregates — were answered by arms of the same
`repositoryRoute` switch, with their request builders split across
`dispatch_cicd.go` and `dispatch_cicd_aggregates.go`. Family membership, both
aggregate builders, and the private `provider_run_id`-to-`run_id` fallback now
live under `internal/mcp/cicd`, and `dispatch_cicd_aggregates.go` is gone. Root
keeps every tool definition and its assembly position, global fanout order, the
thin `cicdRoute` adapter, dispatch, authorization, transport, timeouts, response
budgets, envelopes, summaries, and telemetry. The adapter is consulted directly
after the package-registry one at the top of `repositoryRoute`, so the
repository router keeps its own position in the global chain and no other
family's resolution order changes. The three tool names are disjoint from the
package-registry family and from the remaining switch arms, and the 162-tool
order, the advertised schemas, the `limit` defaults of 50 and 100, the `offset`
default of 0, the `group_by` fallback to `outcome`, and every selected method,
path, and query key remain unchanged.

The CODEOWNERS ownership route family is the third Wave 2 MCP extraction and
the smallest: one tool, one arm of the same `repositoryRoute` switch. Its
request builder sat in `dispatch_codeowners.go` beside a private
`optionalIntString` helper that nothing else called. Family membership, the
builder, and that helper now live under `internal/mcp/codeowners`, and
`dispatch_codeowners.go` keeps only the thin `codeownersRoute` adapter. Root
keeps the tool definition and its assembly position, global fanout order,
dispatch, authorization, transport, timeouts, response budgets, envelopes,
summaries, and telemetry. The adapter is consulted directly after the CI/CD one
at the top of `repositoryRoute`, so the repository router keeps its own position
in the global chain and no other family's resolution order changes. The one tool
name is disjoint from the package-registry and CI/CD families and from the
remaining switch arms, and the 162-tool order, the advertised schema, the
`limit` default of 50, and every selected method, path, and query key remain
unchanged. The `optionalIntString` semantics move verbatim: `after_order_index`
is the numeric leg of a three-part keyset cursor the handler admits only whole,
so an absent key stays the empty string rather than taking a default, which is
why the child reimplements the helper against `routecontract.Arguments` instead
of collapsing it into `IntOr`.

The secrets/IAM posture route family is the fourth Wave 2 MCP extraction: five
tools, five arms of the same `repositoryRoute` switch -- one fewer than the
package-registry family moved -- with all five request builders sitting together
in `dispatch_secrets_iam.go` and no private helper between them. Family membership and all five builders now live
under `internal/mcp/secretsiam`, and `dispatch_secrets_iam.go` keeps only the
thin `secretsIAMRoute` adapter. Root keeps every tool definition and its
assembly position, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter is
consulted directly after the CODEOWNERS one at the top of `repositoryRoute`, so
the repository router keeps its own position in the global chain and no other
family's resolution order changes. The five tool names are disjoint from the
package-registry, CI/CD, and CODEOWNERS families and from the remaining switch
arms, and the 162-tool order, the advertised schemas, the `limit` default of 50,
and every selected method, path, and query key remain unchanged.

What makes this family worth reading is that it is not uniform. The four
listings page, so each carries `limit` plus its own keyset cursor and filters.
`count_secrets_iam_posture` is a scope-anchored aggregate over the whole
posture, so it carries `scope_id` and nothing else -- no `limit`, no cursor, no
filter. The tempting edit is to give it a `limit` for symmetry with its four
siblings; that compiles and reads like a consistency fix. It would not cap the
total either -- the handler reads only `scope_id` -- so the key would be inert
and would advertise a bound the endpoint does not honor. Two guards fail on a mutant that adds one and
on one that drops `scope_id`: the child's own summary test and the
dispatch-level `TestSecretsIAMPostureSummaryStaysScopeOnlyThroughDispatch`. The
adapter parity test is not one of them and cannot be -- it builds its expected
value by calling the same child selector, so a mutation moves both sides
together. What it does prove is that the adapter transcribes method, path, body,
and query faithfully, which is a different claim.

The observability-coverage route family is the fifth Wave 2 MCP extraction and
returns to the single-tool shape: one tool, one arm of the same
`repositoryRoute` switch, one request builder in
`dispatch_observability_coverage.go` with no private helper beside it. Family
membership and the builder now live under `internal/mcp/observabilitycoverage`,
and `dispatch_observability_coverage.go` keeps only the thin
`observabilityCoverageRoute` adapter. Root keeps the tool definition and its
assembly position, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter is
consulted directly after the secrets/IAM one at the top of `repositoryRoute`, so
the repository router keeps its own position in the global chain and no other
family's resolution order changes. The one tool name is disjoint from the
package-registry, CI/CD, CODEOWNERS, and secrets/IAM families and from the
remaining switch arms, and the 162-tool order, the advertised schema, the
`limit` default of 50, and every selected method, path, and query key remain
unchanged.

What makes this family worth reading is the width of a single route.
`list_observability_coverage_correlations` carries twelve query keys -- a
cursor, a limit, and ten filters spanning scope, provider, coverage signal and
status, observability object, source and resource class, outcome, and both
target anchors -- which is more than any other route the repository router
selects. The handler reads each key by name and has no catch-all, and a key
dropped in the move fails two different ways. `limit` is required and a scope
anchor is required, so losing either returns 400. Losing a plain filter returns
200 and widens the caller's page to rows they filtered out, which reads as a gap
the graph does not have. That is why the child tests and the dispatch-level test
assert all twelve keys individually as well as by exact request: a loud failure
and a silent one need the same per-key coverage.

The container-image identity route family is the sixth Wave 2 MCP
extraction, and the only one whose request builders did not come out of a file
named for the family. `containerImageIdentitiesRoute` and
`containerImageTagHistoryRoute` sat in `dispatch_supply_chain.go` beside six
supply-chain builders that stay there, while the count and inventory builders
sat alone in `dispatch_container_image_aggregates.go`. All four now live under
`internal/mcp/containerimage`; the aggregates file is deleted and
`dispatch_container_image.go` takes its place holding only the thin
`containerImageRoute` adapter. Root keeps the four tool definitions and their
assembly positions, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter is
consulted directly after the observability-coverage one at the top of
`repositoryRoute`, so the repository router keeps its own position in the
global chain and no other family's resolution order changes. The four tool
names are disjoint from the five earlier families and from the remaining
switch arms, which drop from 30 to 26, and the 162-tool order, the advertised
schemas, the `limit` defaults of 50 and 100, and every selected method, path,
and query key remain unchanged.

The root file set is the one thing this family changes that the previous five
did not. Deleting `dispatch_container_image_aggregates.go` and adding
`dispatch_container_image.go` leaves `internal/mcp` at the same 106 non-test Go
files it had before, so the dirgate count still matches while the name set does
not. That is exactly the same-count swap the grandfather digest exists to
catch, and it is why this commit carries a re-pin whose count column is
unchanged and whose digest column moves.

What makes this family worth reading is that its four routes look
interchangeable and are not. Tag history is served from
`/api/v0/images/tag-history`, not the
`/api/v0/supply-chain/container-images/identities` prefix its three siblings
share, because `TagHistoryHandler.Mount` registers it there; folding it onto
the sibling prefix reads like tidying and selects a path the query mux does not
serve. The count route carries no `limit` and no `offset`, because its handler
reads neither: a page size sent there would be inert rather than enforced.
`limit` defaults to 50
on the listing and tag history but 100 on the inventory. And the four routes
fail differently when a key goes missing: the listing 400s without `limit` and
without one of its five scope anchors, tag history 400s without both
`repository_id` and `tag` since the handler composes them into the `image_ref`
it anchors on, and the two aggregates require nothing at all, so a lost filter
there returns 200 over a wider scope and quietly drops that key from the
`scope` block the response echoes back. The `group_by` fallback to `outcome` is
a fourth shape again: `containerImageIdentityInventory` applies the same
default itself, so removing the fallback changes no answer, while changing it
to another dimension changes every ungrouped caller's answer. The child tests
and the dispatch-level test therefore assert each route's keys individually
against literal expectations, not against the child selector, since the
adapter parity test builds both of its sides from that same selector.

The supply-chain-impact route family is the seventh Wave 2 MCP extraction.
Its four tools -- the bounded vulnerability finding listing plus its
whole-scope count, grouped inventory, and single bounded explanation -- were
answered by four arms of the same `repositoryRoute` switch. Two request
builders, `supplyChainImpactFindingsRoute` and
`supplyChainImpactExplanationRoute`, sat in `dispatch_supply_chain.go` beside
four supply-chain builders that stay there; the other two, plus the
eighteen-filter helper they share, sat alone in
`dispatch_supply_chain_aggregates.go`. Family membership and all four
builders now live under `internal/mcp/supplychainimpact`; the aggregates file
is deleted and `dispatch_supply_chain_impact.go` takes its place holding only
the thin `supplyChainImpactRoute` adapter. Root keeps the four tool
definitions and their assembly positions, global fanout order, dispatch,
authorization, transport, timeouts, response budgets, envelopes, summaries,
and telemetry. The adapter is consulted directly after the container-image
one at the top of `repositoryRoute`, so the repository router keeps its own
position in the global chain and no other family's resolution order changes.
The four tool names are disjoint from the six earlier families and from the
remaining switch arms, and the 162-tool order, the advertised schemas, the
`limit` defaults of 50 and 100, the `offset` default of 0, the `group_by`
fallback to `impact_status`, and every selected method, path, and query key
remain unchanged.

As with the container-image extraction, deleting
`dispatch_supply_chain_aggregates.go` and adding
`dispatch_supply_chain_impact.go` is a same-count file swap: `internal/mcp`
holds the same non-test Go file count it had before, so the dirgate re-pin
here is digest-only.

What makes this family worth reading is the `include_suppressed` filter. It
is not a plain string like its neighbors: the handler treats a missing key as
its documented `false` default and only rejects a non-true/false value, so the
route must forward `"true"` or `"false"` when the caller set an explicit bool
and omit the key entirely otherwise. `routecontract.Arguments.BoolOr` cannot
express that three-state contract -- it collapses "the caller never set this"
into the fallback, which is indistinguishable from an explicit `false` -- so
the child package carries its own `boolStr` helper rather than reusing
`BoolOr`, the same shape the root dispatcher used before extraction. The
listing and the two aggregates share this helper and the same eighteen
filters; the explanation carries none of them, since it answers exactly one
finding, one no-evidence explanation, or one ambiguous-scope refusal rather
than a page.

The security-alert reconciliation route family is the eighth Wave 2 MCP
extraction. Its three tools -- the cursor-paged listing of reducer-owned
provider security-alert reconciliations plus its whole-scope count and
grouped inventory -- were answered by three arms of the same
`repositoryRoute` switch. The listing builder,
`securityAlertReconciliationsRoute`, sat in `dispatch_supply_chain.go` beside
three supply-chain builders that stay there; the two aggregate builders sat
alone in `dispatch_security_alert_aggregates.go`. Family membership and all
three builders now live under `internal/mcp/securityalert`; the aggregates
file is deleted and `dispatch_security_alert.go` takes its place holding
only the thin `securityAlertRoute` adapter. Root keeps the three tool
definitions and their assembly positions, global fanout order, dispatch,
authorization, transport, timeouts, response budgets, envelopes, summaries,
and telemetry. The adapter is consulted directly after the
supply-chain-impact one at the top of `repositoryRoute`, so the repository
router keeps its own position in the global chain and no other family's
resolution order changes. The three tool names are disjoint from the seven
earlier families and from the remaining switch arms, which drop from 22 to
19, and the 162-tool order, the advertised schemas, the `limit` defaults of
50 and 100, the `offset` default of 0, the `group_by` fallback to
`reconciliation_status`, and every selected method, path, and query key
remain unchanged.

As with the container-image and supply-chain-impact extractions, deleting
`dispatch_security_alert_aggregates.go` and adding
`dispatch_security_alert.go` is a same-count file swap: `internal/mcp` holds
the same non-test Go file count it had before, so the dirgate re-pin here is
digest-only.

What makes this family worth reading is that the listing is scope-anchored
while its two aggregate siblings are not. `SecurityAlertReconciliationFilter
.hasScope` requires one of `repository_id`, `provider`, `package_id`,
`cve_id`, or `ghsa_id`; `provider_state` and `reconciliation_status` do not
count as anchors on their own, so a caller who sets only those two still
400s on the listing. The count and the inventory require nothing at all, so
a lost filter there returns 200 over a wider scope and quietly drops that
key from the `scope` block the response echoes back. This family carries no
`boolStr`-shaped tri-state filter -- every key is a plain string or a paging
integer -- so, unlike supply-chain-impact, the child needs no local
reimplementation beside `routecontract.Arguments`.

**cmd/eshu (233):** `package main` — subdirectories are impossible by
language rule. The lever is extracting business logic to new
`internal/cli/<family>` packages, leaving thin cobra RunE wrappers —
which the package's own AGENTS.md already demands. ~20 families measured;
all clean except the local_host/local_graph supervisor cluster (31 files,
real bidirectional cycle — extract as ONE `localsupervisor` unit or leave
last).

**parser (259 root):** the split already happened — 34 language
subpackages hold 734 files. Root keeps the Engine/Registry/Runtime
dispatcher (by design: languages must not import the parent). The 27
`<lang>_language.go` glue files are Engine methods and CANNOT move without
wiring the already-defined-but-unused LanguageProvider seam — a refactor
to schedule separately, not a file move. What CAN happen now: normalize
straggler filenames and convert per-language root tests into external
`_test` packages inside the existing language dirs, with the two parser
ledgers updated in lockstep. Lowest priority of the seven.

**query (1,903):** the architecture already fits — ~60 handler types, each
with its own Mount(). Phase 0 decision needed first: 284 external files
(86 in mcp dispatch) reference `query.<Type>`; the research recommends
root type aliases (`type SupplyChainHandler = supplychain.Handler`) so
external code compiles unchanged, burned down later. That alias alone does
not compile — see the acyclic-boundary prerequisite below, which has to land
first. Then clean families first: supplychain(~183), code(~172),
contentread(42), packagereg(32).
Tangled families (impact ← repository/service/deployment_trace call its
unexported helpers) need the helper seam exported before their move. Root
keeps: APIRouter/Mount, compatibility aliases and `Write*` wrappers,
capability rows in the existing `contract_*` files, openapi.go assembly + the 101
`openapi_paths_*` constants, and the two cross-cutting test sweeps
(auth_scoped_routes 41 files, graph_read_error 17).

**reducer (1,269):** hub-and-spoke with a small hub: registry.go, the
defaults_additive_domains wiring (11 files), domain.go/intent.go,
shared_projection harness (26), batch-insert helpers. ~30 largest families
symbol-measured: most are clean (containerimage 81, cicdrun 28, secalert,
iamcan, searchdoc, secretsiam, sbomattest, awscloudruntime, tfconfigstate,
the six code-intelligence domains, ~15 per-cloud-resource domains). Three
proven traps: supply_chain_impact ↔ supply_chain_suppression are
bidirectionally type-coupled (one subpackage, or hoist shared types);
code_call_materialization needs code_call_language's unexported resolver
registry exported first; `service_materialization_{docs,vulnerabilities,
incidents}.go` are misnamed ServiceCatalogCorrelationHandler methods —
they move with svccatalog, proving every cluster gets measured before
moved. ~400 external files import reducer; storage/postgres names
family-specific types — whole-module grep before each family move.

### Prerequisite for four packages: an acyclic boundary

A reviewer caught this in query and reducer, and applying the same check to the
rest of Part 3 found it in projector and mcp too. It is the one thing in this
plan that does not compile as written.

The shape is always the same. Root calls into a symbol that is scheduled to move
out, and the moved symbol needs a type that stays in root. Either edge alone is
fine. Both at once is an import cycle, and Go refuses to build it.

| Package | Root reaches into the family | Family needs from root |
|---|---|---|
| query | the alias `type SupplyChainHandler = supplychain.Handler` plus router wiring | `querycontract.GraphQuery`, `ContentStore`, profiles, envelopes, HTTP helpers, and family-local capability registration |
| reducer | 48 `.Handler = <Family>Handler{}` construction sites across 10 of the 11 `defaults_additive_domains*.go` wiring files — e.g. `defaults_additive_domains_correlation.go:66-67`, which calls `containerImageIdentityDomainDefinition()` and builds `ContainerImageIdentityHandler{}` | `Intent`, `Result` and the `Handler` interface (`container_image_identity.go:52,73`) |
| projector | `scope_generation_intents.go` has 44 reducer-intent builder call sites, defined across 41 family files | `ReducerIntent` (`runtime.go:50`) |
| mcp | `types.go` has 42 `append(tools, <domain>Tools()...)` call sites | `ToolDefinition` (`types.go:7`) |

Collector and coordinator are genuinely clear. Collector families are
constructed from external `cmd/` binaries. Coordinator scheduler families use
the dependency-neutral `plannercontract` helper while root retains their
Planner interfaces and service methods, so an extracted scheduler does not
need to import root.
`cmd/eshu` has a different constraint rather than a cycle — it is `package main`
and nothing can import it, so logic extracted to `internal/cli/<family>` cannot
call back into the shared CLI helpers at all. Either those helpers move too, or
the extracted logic must not need them.

Two ways out, and they are not interchangeable:

- **Hoist the shared contracts** — ports, envelopes, `DomainDefinition`,
  `Domain`, `Intent`, `Handler`, `ReducerIntent`, `ToolDefinition` — into a
  dependency-neutral package below both root and the families. This works for
  all four.
- **Invert registration** so root never names a family symbol: the family
  registers itself, and a wiring package above both owns the import edge. This
  works for reducer, projector and mcp, where root's only edge is a call. It
  does NOT work for query: the alias is itself root naming a family symbol, and
  it has to stay in root or the 284 external `query.<Type>` references stop
  compiling, which is the whole reason the alias exists.

Until this lands, read `clean` in the family tables as what it measures: zero
coupling to other families. It does not mean ready to move.

This also breaks Part 4's order below, which moves projector and mcp well before
query and reducer. Either the boundary lands first for every affected package, or
those moves wait.

## Part 4: execution model

- **Order:** gate PR → hardening PR → collector → projector+coordinator →
  mcp → cmd/eshu → query → reducer. Parser last, optional. Small packages
  move while normal lanes run (per-package claim, same as issue claims);
  **query and reducer each get an all-lanes-quiet window** — their moves
  conflict with everything.
- **One owner** for the whole program. Two agents inventing taxonomies
  produce two taxonomies.
- **Per move PR:** `git mv` (history follows); one family per PR; the
  three doc files (doc.go, README.md, AGENTS.md) for every new directory
  (the package-docs gate enforces this); gate/spec path updates in the
  SAME PR; behavior-preserving proof = whole-module build + full package
  tests + golden-corpus gate byte-identical + `go vet` + route/openapi
  parity where applicable. No logic changes ride along with moves, ever.
- **Timing:** after current lanes drain, before Epic M (multi-tenancy),
  feeding directly into #4047/#4398. Epic M then lands on the new layout.

## Part 5: what this buys the modularization program

Extraction grades from the research become the repo-split roadmap:

- `clean` families (query/supplychain, query/code, reducer/containerimage,
  collector/gitrepo leaves, projector provider intents, coordinator
  schedulers, most cli families) = future module/repo candidates with
  measured-zero internal coupling.
- `tangled` families = the dependency-inversion backlog, each with its
  named blocker (impact helper seam, Service decomposition, dispatch hub
  extraction, LanguageProvider wiring, supplychain type hoist).
- `shared-core` sets = the de-facto public API of each future module;
  what stays in root today is what a split repo would have to export
  tomorrow. #4047's "extraction readiness gate" can assert exactly this.
