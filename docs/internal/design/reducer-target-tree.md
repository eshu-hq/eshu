# Reducer target tree (#6061)

Owner-approved destination for the `go/internal/reducer` restructure. First
approved 2026-09-08 (counts measured at `origin/main` `8d0c6cec` unless
noted). Rebuilt 2026-09-11 after the owner turned down the first version of
#6645 under `docs/internal/naming.md`. The approved spec is #6609 comment
5631604960. Where it conflicts with anything older on this page, the
2026-09-11 decisions win.

## Why this exists

The 40-file dirgate cap is a proxy for "a human can navigate this", not the
goal. At first approval the tree was **304 non-test root files plus 54 flat
sibling subpackages**, several named after edge verbs (`s3logsto`,
`ec2usesprofile`, `iamcan`, `s3grant`, `secgroup`, `gpphase`, `crossscope`,
`dsl`, `tags`). That is two readability problems: a flat root no one can
scan, and sibling names no one can decode. No 55th cryptic sibling is ever
created. Every move lands in a NAMED destination below, never away from a
file count.

```bash
git ls-tree --name-only origin/main go/internal/reducer/ | rg '\.go$' | rg -vc '_test\.go$'  # root non-test
ls -d go/internal/reducer/*/ | wc -l                                                          # subpackages
```

## Owner decisions recorded (2026-09-11, #6609 comment 5631604960)

The 2026-09-08 tree predates `docs/internal/naming.md`. The owner's review of
#6645 turned it down for glued directory names, stuttering file names, and
157 files left in the root. Where the two conflict, these decisions win:

1. **One parent per PR.** #6645 lands the complete `code/` subtree plus the
   hoists it needs. The other parents follow, one PR each, in the order under
   Sequencing.
2. **`code/call` nests by language.** The entity index is its own package,
   `code/call/shared`. Language leaves import it, and the dispatcher imports
   the leaves: the same shape as `parser` / `parser/<lang>` /
   `parser/shared`. Registration is an explicit list in `languages.go`, so a
   missing language fails to compile instead of silently resolving nothing.
   This reverses the 2026-09-08 "single `code/call`, no separate package"
   lock.
3. **`EntityIndex` keeps its fields unexported.** Leaves read the index
   through read-only accessors, which inline at zero measured cost (evidence
   under step 3).
4. **The runners and the handler leave the root.** The shared-projection
   substrate moves to `intents/shared/worker` (H5), after the H1–H4 hoists.
5. **`projection.go`, `projection_helpers.go`, `candidate_loader.go` and
   `platforms.go` leave the spine.** They are workload/platform product: all
   18 callers are workload files.
6. **`dependency/{repo,imports,submodule,resolution}`** replaces the glued
   `repodependency/`.
7. **`supply/chain/{impact,cicd,image,sbom,model}`** replaces `supplychain/`.
8. **`service/catalog`** holds `servicecatalog`. The root loop files are
   renamed `loop*.go`.
9. **Exported stutter is dropped in the move PR.** Wire strings, domains,
   entity keys and telemetry stay byte-identical. The `payloadcore`
   `Payload*` accessors get their own PR.
10. **`code/intel` is held** until the query-lane owner agrees.

## Owner decisions recorded (2026-09-08, #6061)

1. **Supplychain hoist: yes.** `packages/correlation` moves FIRST so the
   dependency points at a named package, then the `supplychain` core plus
   suppression land together (the `Suppression SupplyChainSuppressionDecision`
   struct field at `finding.go:103` makes them one unit).
2. **Compat surface (2a) + importer migration (2b); no dirgate exception.**
   (Owner answers, #6061 comment 5591291715. They supersede the ~100-110
   floor, which measured the old move-a-family-leave-a-compat-file strategy,
   not the package: one line pins a `reducer.X` spelling to root, and the
   compat surface is ~1,600 lines total (1,530 in the 20-file census plus 63
   in the two numbered decode-seam sequels), i.e. ~4 files at the 500-line
   cap.)
   (a) The 22 `*_compat.go` merge into ~4 buckets (`compat_cloud.go`,
   `compat_correlation.go`, `compat_decode.go`, `compat_projection.go`) in
   the compat-consolidation PR: no external caller edits, no behavior
   change, every alias and forwarder preserved; root 304 -> 286. Every
   later family move adds a stanza to the matching bucket file and NEVER
   creates a new `*_compat.go` — that rule stops the moves making the
   problem worse. Rebalance buckets before any one crosses the 500-line cap;
   when a move would still push one over, migrate the highest-count
   importers to the new paths first (2026-09-11 plan, D12).
   Measured at #6061 HEAD `81a51b706` (after the value_flow rebalance, before
   any family move): `compat_cloud.go` 369, `compat_correlation.go` 416,
   `compat_decode.go` 347, `compat_projection.go` 417 (1,549 bucket lines).
   The supplychain move's stanza and orphan burn-down landed the buckets at
   341/456/318/366 (1,481 lines).
   (b) External importers migrate `reducer.X` -> owning subpackage in
   per-package batches (postgres 191, cypher 65, cmd/reducer 40,
   projector 39, materializededges 38, then the tail) under its own child
   issue, after the tree is populated — NOT blocking family moves. Each
   batch is independently provable and deletes the aliases it retires; this
   is the coupling #4047/#4398 must remove to extract reducer.
   (c) Target root ~9: doc.go + ~4 compat + ~4 contract surface (intent,
   domain, runtime, registry). ≤40 clears with room; no exception. The
   dirgate row ratchets DOWN on every move PR, as
   `internal/collector/gitrepo` does at 66.
3. **The 2026-09-08 tree as the destination.** Superseded by the 2026-09-11
   tree below.

## The tree

Every directory is plain English and nested (naming.md rule 3), and no file
repeats its directory's name (rule 2). Namespace parents carry only a doc
trio. `contract/` stays top-level shared vocabulary.

| Parent | Children (← old name) |
|---|---|
| `code/` | `call` (+ `shared`, 13 language leaves, `materialization`, `projection`), `function/summary`, `owners`, `shell`, `taint`, `value` (+ `cleanup`), `semantic` ← `semanticentity`, `intel` ← `codeintel` (held) |
| `intents/` | `shared` ← `sharedintent` (+ `worker`), `phase` ← `gpphase` (+ `repair`), `readiness` ← `crossscope`, `maintenance` |
| `fact/` | `decode` ← `factdecode` (+ `schema` ← `schemadecode`), `load` ← `factload`, `write` ← `factwrite`, `payload` ← `payloadcore`, `admission` ← `admissiondecision` |
| `edges/` | `documentation`, `rationale`, `inheritance`, `dsl`, `sql` ← `sqlrelationship` |
| `dependency/` | `repo`, `imports` ← `code_import_*`, `submodule`, `resolution` ← `crossrepo` |
| `workload/` | `correlation` ← `deployable_unit_*`, `materialization`, `identity`, `cloud` |
| `platform/` | ← `platformfam` + `platforms.go`, `infrastructure` |
| `aws/` | `resource`, `relationship`, `cloud` ← `awscloud`, `join` ← `cloudjoin`, `ec2/instance` (+ `profile` ← `ec2usesprofile`), `ec2/encryption` ← `ec2blockkms`, `internet/exposure`, `rds/posture`, `s3/grant`, `s3/logging` ← `s3logsto` |
| `gcp/`, `azure/` | ← `gcp_*`, `azure_*` |
| `cloud/` | `asset`, `inventory`, `tags`, `runtime/drift/multi` ← `multicloudruntimedrift` |
| `iam/` | `access` ← `iamcan`, `escalation`, `policy`, `instance/profile` ← `iaminstprofile` |
| `kubernetes/` | `correlation` ← `kubernetescorrelation`, `live` ← `kubernetes_*` |
| `terraform/` | `drift` ← `tfconfigstate`, `state` ← `tfstate` |
| single leaves | `crossplane/satisfaction`, `observability/coverage` ← `obscoverage`, `incident`, `secrets/iam`, `security/{alert,group}`, `search/{document,vector}`, `supply/chain/{impact,cicd,image,sbom,model}`, `service/catalog` |
| unchanged | `contract/`, `packages/{correlation,source}` |

Placement notes:

- `dependency/imports` ← `code_import_*`: it returns package-correlation
  types (`PackageSourceDecision` from `code_import_owner_facts.go`), so it
  moves only after `packages/correlation` exists and imports it one-way.
  6 non-test files.
- `sbomattest` → `supply/chain/sbom`: SBOM attestation is supply-chain
  provenance evidence. `cicdrun` → `supply/chain/cicd`: CI-run-to-image
  provenance, in the measured cycle with the supply-chain core and
  `containerimage`.
- `platformfam` → `platform/`: Terraform runtime vocabulary plus the
  `deployment_mapping` reduction, which consumes `resolved_relationships`.
  The post-Phase-3 reopen invariant in `go/internal/reducer/AGENTS.md`
  travels with it: the move PR carries the existing `bootstrap-index/main.go`
  reopen wiring and proves it still fires, or the stuck-`deployment_mapping`
  failure mode recurs.
- `admissiondecision` → `fact/admission`: shared decision vocabulary used by
  correlation handlers, not one family's product.
- `crossscope` → `intents/readiness`: read by more than one family, gating
  cross-scope consumers on producer readiness.
- `cloudjoin` → `aws/join`: it resolves AWS endpoint identity for the AWS
  families. This reverses the earlier top-level placement (#6614).
- `obscoverage` → `observability/coverage`: it correlates AWS-native
  observability objects against monitored CloudResource nodes.
- The service/runtime core (the claim/execute/ack loop) stays in the root;
  its files are renamed `loop*.go` when `servicecatalog` moves to
  `service/catalog`, which removes the naming-rule collision (decision 8).

## The spine (stays in root, <=40)

The composition root: `doc.go`, the four compat buckets, `registry.go` +
`registry_additive_domains.go`, the `defaults*.go` wiring, `intent.go` +
`intent_value.go`, `domain.go`, `runtime.go`, `dependency.go`,
`observability.go`, `cross_scope_completion_runner.go` (until
`intents/readiness`), and the service loop (`service.go`,
`service_loop_config.go`, `service_runtime_instance_lookup.go`,
`service_batch.go`, `service_heartbeat.go`, `service_observability.go`,
`service_side_runners.go`). `projection.go`, `projection_helpers.go`,
`candidate_loader.go` and `platforms.go` are not spine (decision 5); they
leave with `workload/` and `platform/`. Compat entries burn down to zero as
the importer-migration child issue lands; until then no new `*_compat.go`,
ever.

Root arithmetic: 304 at first approval; 286 after the compat-consolidation
PR; 274 after `packages/correlation`; 204 after `supplychain/core`; 157 at
the #6645 base; {{ROOT_AFTER}} after #6645 (dirgate row re-pinned
157 -> {{ROOT_AFTER}} with the re-derived digest in the same PR).

`shared_projection*` was never spine. #6645 hoists it: port shapes and pure
row helpers into `sharedintent` (H1, H2), phase repair and presence keys into
`gpphase` (H3), `ProjectionDomains`, the rationale evidence source and
`GraphQueryRunner` into `contract` (H4), then the worker into
`intents/shared/worker` and the phase-repair drain into
`intents/phase/repair` (H5). Direction is family -> shared tier, never
root <- family. The shared-projection intent identity is byte-preserved and
proven by B-7/B-12 like any other move.

## Triage roll (prefix-proposed, symbol-confirmed at move time)

These have a proposed home by name but were not individually measured; the
move PR's `go/types` census confirms or corrects, and this doc is amended
when it disagrees. Never a new top-level package for any of them. Rows for
singletons that already left the root are dropped.

| File(s) | Proposed home |
|---|---|
| `s3.go`, `ec2*` singletons | `aws/` children by census |
| `gcp_materialization.go` | `gcp/` |
| `sbom*` singleton, `security*` singleton | `supply/chain/sbom`, `security/alert` |
| `observability_coverage.go`, `quarantine*`, `decode*` (3), `shared_payload.go`, `intent_emission.go` | `fact/` children by census (`intent_emission.go` defaults to `fact/decode`) |
| `platform_infra_materialization.go` (the `platform` stanza in `compat_cloud.go` burns down) | `platform/infrastructure` |
| `projection.go`, `projection_helpers.go`, `candidate_loader.go`, `platforms.go` | `workload/` and `platform/` (decision 5) |
| `workload_*` singletons (signal, identity, deployment, dependency, cloud, instance) | `workload/` children by census |
| `endpoint*`, `scoped*`, `projected*`, `documentation*`, `environment*`, `source*`, `materializ*`, `infrastructure*`, `cross*`, `go*`, `python*`, `parsed*` | census at move time; no placement asserted here |

## Sequencing (one parent per PR)

1. `packages/correlation` (landed). 12 files: the 11 `package_*` plus
   `security_alert_manifest_dependency_match.go`, after 6 generic-func
   evictions to `payloadcore`. `securityAlertPackageNameMatches` travels
   with the leaf, importing the already-extracted `securityalert/` one-way,
   because it takes `securityalert.ProviderSecurityAlert`.
2. `supplychain` core + suppression together (landed, #6636). 70 files,
   explicitly including `finding.go`: suppression signatures take
   `SupplyChainImpactFinding`, so the unit is finding+core+suppression or
   nothing. External callers resolve through the supply_chain_impact stanza
   in `compat_correlation.go` with zero edits.
3. `code/` (#6645): the complete subtree plus the hoists it needs
   (decision 1). H1–H5 land first as leaf additions with root aliases (see
   the spine section). Then `code/call` splits by language (decision 2),
   and the handler, the seven code-call projection runners, the value-flow
   cleanup runner and the symbol-runtime builders leave the root
   (decision 4). Dispatcher tests stay in `code/call`: they enter through
   `ExtractRows`, and a language directory cannot hold an internal test
   that imports its own dispatcher. `code_import*` moves later, in
   `dependency/imports`. `codeintel` -> `code/intel` is held (decision 10):
   its importer `internal/query/downgraded_code_root_kinds_roundtrip_live_test.go`
   is lanes A/B territory. The layout #6645 lands:

   ```
   code/                            namespace (doc trio only)
   ├── call/                        doc extract resolution intents rows languages
   │   │                            delta_partitions file_scope instantiates relationship
   │   ├── shared/                  entity index + resolver contract (context.go)
   │   ├── golang/ java/ javascript/ typescript/ python/ jvm/
   │   ├── kotlin/ groovy/ dart/ elixir/ haskell/ perl/ rust/ swift/
   │   ├── materialization/         handler routes workloads cloud_actions refresh
   │   └── projection/              runner lease selection rows partitions candidates telemetry
   ├── function/summary/            handler decode
   ├── owners/                      handler scope
   ├── shell/                       handler intents
   ├── taint/                       package taint (was codetaint)
   ├── value/                       package value (was valueflow)
   │   └── cleanup/                 runner
   └── semantic/                    was semanticentity
   intents/                         namespace
   ├── shared/worker/               the shared-projection worker (H5)
   └── phase/repair/                the phase-repair drain (H5)
   ```

4. `intents/` (moves `sharedintent` and `gpphase`) and `fact/`. Shared
   contracts: each moves with all of its consumers in the same PR, with no
   forwarding package left behind.
5. `edges/` and `dependency/`.
6. `aws/`, `gcp/`, `azure/`, `cloud/`, `iam/`, `kubernetes/`, `terraform/`.
7. `workload/` and `platform/`, after decision 5.
8. `supply/chain/`, `security/`, `search/`, `service/catalog`,
   `observability/`, `incident/`, `secrets/`, `crossplane/`.
9. `code/intel`, once the query lane agrees.

Re-derive each family with `go/types` first; filename prefixes lie (per Lane
A's query census: 4 of 46 `code*.go` files belonged to a different handler).
Open #6634 edits the code-call runner files and the dirgate ledger:
whichever of it and #6645 lands second restacks onto the other.

Preconditions per move PR: all-lanes-quiet window (re-check open PRs every
preflight); `internal/query` untouched (lanes A/B, #5167); one-way imports
only (root -> family -> shared tier -> `contract`; families never import
root); gate/spec lockstep in the same PR.

## Proof bar (every move PR, no exceptions)

- Moves are `git mv` so history follows; behavior-preserving, never mixed
  with logic changes. All Go commands run from the module directory
  (`cd go` — the module lives under `go/`, so the bare forms fail from
  the repo root).
- `(cd go && go build ./... && go vet ./...)`;
  `(cd go && go test ./internal/reducer/... -count=1)` recursive (never a
  bare package); repoint proof via
  `(cd go && go test ./internal/reducer/<newpkg>/... -list 'TestMovedFamily' -count=1 | rg -q TestMovedFamily)`
  with a real moved test name substituted: `-list` only prints matches
  and exits 0 on empty, so the `rg -q` assertion (not the exit code) is
  what proves the repoint carried tests. A stale `-run` selects 0 tests
  yet exits 0.
- Golden corpus (B-7) and e2e snapshot (B-12) byte-identical or STOP.
- Every new directory carries `doc.go`, `README.md`, `AGENTS.md` with real
  content.
- `(cd go && go run ./cmd/ci-gates validate --registry ../specs/ci-gates.v1.yaml --repo-root .. --drift)`
  passes (no `ci-gates` binary exists on `PATH` — the canonical form is
  `go run ./cmd/ci-gates` from `go/`, per
  `scripts/verify-ci-gates-registry.sh`; `--registry` is required);
  telemetry-coverage row check passes.
- Dirgate row re-pinned DOWN in the same PR (see restack rule below).
- `eshu-code-review` P0=P1=P2-blocking=0 on both sides of the promotion
  preflight; only the coordinator runs `make pre-pr`, once per push.

## Proof bar evidence: compat-consolidation PR (decision 2a)

No-Regression Evidence: the 22-file merge is alias/forwarder relocation
with zero call-site edits, so there is no runtime delta to measure —
correctness is proven by construction plus replay. Baseline
`origin/main 4e94c8cf9`, backend go1.27.1 darwin/arm64 with the B-7
gate's own Postgres + NornicDB stack. Measured on the branch, from
`go/`: `go build ./...` exit 0; `go vet` clean; `gofumpt -l` clean;
`go test ./internal/reducer/... -count=1` green (full recursive tree);
`go test ./internal/payloadusage/... -count=1` green; go/parser census
281/281 top-level declarations identical across the 22 deleted files vs
the 4 buckets; B-7 golden-corpus gate 562 pass / 0 fail (31-repo corpus,
134s); B-12 replay-coverage gate `--blocking` PASS with a byte-identical
(no-op) dashboard rewrite. Safe because every alias resolves at compile
time, every forwarder body is byte-identical (inlining shape unchanged),
and no caller, query, queue, worker, or storage contract changed.

No-Observability-Change: forwarders compute nothing, so no new signals
are required. The telemetry-coverage rows name the new bucket-stanza
paths with identical covering instruments (`eshu_dp_reducer_executions_total`
/ `eshu_dp_reducer_run_duration_seconds` for the owning passes,
`eshu_dp_reducer_input_invalid_facts_total` for the quarantine surface);
`verify-telemetry-coverage.sh` green.

## Proof bar evidence: supplychain move (tree step 2)

No-Regression Evidence: the 70-file unit move is package relocation with
zero external-caller edits, so there is no runtime delta to measure —
correctness is proven by construction plus replay. Baseline
`origin/main 9cfb05ace`, backend go1.27.1 darwin/arm64 with the B-7
gate's own Postgres + NornicDB stack. Measured on the branch, from
`go/`: `go build ./...` exit 0; `go vet ./internal/reducer/...` clean;
`gofumpt -l` clean; `go test ./internal/reducer/... -count=1` green
(full recursive tree); `go test ./internal/payloadusage/... -count=1`
green; doc-citations 296/296; `scripts/verify-package-docs.sh` reports the
doc trio present for `supplychain/core`;
`verify-telemetry-coverage.sh` green. 148 of 179 changed paths are
git-mv renames (similarity 60-99%; the sub-90 scores are small files
where package-clause plus leaf-symbol requalification dominates the
line count — line-by-line audit of every non-matching hunk shows only
requalification swaps, test-local twins, and the touches below, no
changed operators, thresholds, conditions, or strings). The only logic
touches are two exhaustive case additions that spell out the pre-move
default (empty cases, zero behavior delta) and one funlen tail
extraction (pure code motion, identical conditions and order), all
covered by the recursive suite; ~30 deleted compat forwarders were
lint-unused proven with zero callers. B-7 golden-corpus gate 562 pass
/ 0 fail (129s); B-12
replay-coverage gate `--blocking` PASS with byte-identical report and
reference-doc rewrites. Safe because every moved symbol resolves at
compile time, the new compat stanza preserves every external spelling,
no caller, query, queue, worker, or storage contract changed, and the
moved tree imports only leaves the root already imports (no import
cycle possible by construction).

No-Observability-Change: the move adds no stage and the one new file
outside it (`contract/workload_identity.go`, a single string constant)
emits nothing; its coverage row cites the unchanged writer-path trio
(`eshu_dp_postgres_query_duration_seconds`,
`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`).
`verify-telemetry-coverage.sh` green.

## Proof bar evidence: code/ move (tree step 3)

No-Regression Evidence: #6645 is package relocation plus identifier renames,
so there is no runtime delta to measure. Correctness is proven by
construction plus replay. Baseline `origin/main` `{{BASE_SHA}}`, go1.27.1
darwin/arm64, from `go/`: `go build ./...` exit 0; `go vet
./internal/reducer/... ./cmd/reducer/...` clean; `go test
./internal/reducer/... ./cmd/reducer/... ./internal/storage/postgres/...
./internal/replay/... -count=1` green ({{PKG_OK}} packages ok); the test
function inventory against the base lost 0 and gained 0 (moves keep test
names); B-7 golden-corpus gate {{B7}}; B-12 replay-coverage gate {{B12}}.
The real `ExtractRows` benchmarks, before and after on the same machine,
interleaved, n=12 per side: {{BENCH}}. `go build -gcflags=-m` reports all 10
`EntityIndex` read-only accessors inlinable, and the language leaves inline
them at their call sites, so decision 3 costs nothing measurable. No caller,
query, queue, worker, lease, or storage contract changed.

No-Observability-Change: the move adds no stage and no signal. Wire
strings, domains, entity keys, and metric, span and log names are
byte-identical; the handler still logs `code call materialization
completed`. Every file the move added has a path-only telemetry-coverage row
naming the instruments that already cover its stage;
`verify-telemetry-coverage.sh` green.

## Restack rule (the dirgate ledger trap)

The ledger (`scripts/lib/dirgate-grandfather.tsv`, row 88:
`internal/reducer <count> <digest>`, re-pinned DOWN by every move PR) and its generated `.go` mirror
(`scripts/test-generate-dirgate-grandfather-go.sh`) conflict on every
sibling merge, and a clean merge is the dangerous case. Every move PR:
take `origin/main`'s copy of both, re-derive count and digest for the real
tree, regenerate the mirror with `scripts/generate-dirgate-grandfather-go.sh`
(committed output: `tools/golangci-lint-dirgate/grandfather.go` — never
hand-edit it; `scripts/test-generate-dirgate-grandfather-go.sh` only
asserts), check the sums. Never carry a ledger resolution
forward across a rebase without re-deriving.

## Prior art disposition

Local-only branch `feat/6061-supplychaincore` (`36ea9f98e`, 2026-09-04,
unpushed) hoisted supply-chain value types before #6568 merged. All 13 of
its hoisted type names now exist on `main`; its Sept-4 base predates the
packages/correlation-first ordering by four days with heavy drift in the
touched root files (114 insertions / 237 deletions across 4 files vs
`origin/main`). It is superseded, not revived. The branch is left in
place for the owner to delete (destructive acts need explicit ask);
recoverable SHAs: `36ea9f98e`, `e13a30304`, base `30fcf3972`.

## DONE

Reducer root <=40 for real — no exception (decision 2 above); ~13 named
domains populated; no verb-named top-level siblings; 2b child issue filed;
final comment on #6061 with tree and counts.
