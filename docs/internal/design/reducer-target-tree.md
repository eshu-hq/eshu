# Reducer target tree (#6061)

Owner-approved destination for the `go/internal/reducer` restructure.
All counts measured at `origin/main` `8d0c6cec` unless noted (re-derived
identical after the #6607/#6608/#6613 base move; that move touched only
`internal/query` plus the query ledger row — the reducer tree and its
ledger row are byte-identical).

## Why this exists

The 40-file dirgate cap is a proxy for "a human can navigate this", not the
goal. At approval time the tree is **304 non-test root files plus 54 flat
sibling subpackages**, several named after edge verbs (`s3logsto`,
`ec2usesprofile`, `iamcan`, `s3grant`, `secgroup`, `gpphase`, `crossscope`,
`dsl`, `tags`). That is two readability problems: a flat root no one can
scan, and sibling names no one can decode. No 55th cryptic sibling is ever
created. Every move lands in a NAMED destination below, never away from a
file count.

```bash
git ls-tree --name-only origin/main go/internal/reducer/ | rg '\.go$' | rg -vc '_test\.go$'  # 304 root non-test
ls go/internal/reducer/*.go | rg -v '_test\.go$' | wc -l                                     # 304, same tree locally
ls -d go/internal/reducer/*/ | wc -l                                                          # 54 subpackages
```

## Owner decisions recorded (2026-09-08, #6061)

1. **Supplychain hoist: yes.** `packagecorrelation` moves FIRST so the
   dependency points at a named package, then the `supplychain` core plus
   suppression land together (the `Suppression SupplyChainSuppressionDecision`
   struct field at `supply_chain_impact_finding.go:103` makes them one unit).
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
   problem worse. Rebalance buckets before any one crosses the 500-line cap
   (`compat_correlation.go` lands at 492).
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
3. **This tree: yes as the destination.** PR1 (this doc) commits first; no
   file moves until the doc is merged.

## The tree

Thirteen named top-level domains. Existing subpackages move UNDER a named
parent; directory paths change, Go package contents are not rewritten, and a
package keeps its name unless the verb-name itself is the readability
problem. `contract/` stays top-level (shared vocabulary, never a domain).

| Domain | Children (existing subpackage -> child) | Root buckets landing here (non-test counts) |
|---|---|---|
| `supplychain/` | `core` (new: `supply_chain_impact*` INCLUDING `supply_chain_impact_finding.go` + `supply_chain_suppression*`, 67), `cicd` (`cicdrun`, 11), `image` (`containerimage`, 25), `sbom` (`sbomattest`, 7), `model` (`supplychainmodel`, 2) | `supply_chain*` 67 |
| `packagecorrelation/` | `core` (new: `package_*` 11 + `security_alert_manifest_dependency_match.go`), `source` (`packagesourcecore`, 2) | `package_*` 11, `security_alert_manifest_dependency_match.go` |
| `code/` | `call` (new: `code_call*` 52 minus the #6609 runner-stays set), `intel` (`codeintel`, 5), `taint` (`codetaint`, 11), `value` (`valueflow`, 8 + `code_value*` 2) | `code_call*` 52, `code_value*` 2 (`code_import*` 6 lives in `repodependency/import`, not here) |
| `cloud/` | `aws/s3/logging` (`s3logsto`, 3), `aws/s3/grants` (`s3grant`, 3), `aws/ec2/instance` (`ec2instance`, 5), `aws/ec2/blockkms` (`ec2blockkms`, 4), `aws/ec2/usesprofile` (`ec2usesprofile`, 3), `aws/rds/posture` (`rdsposture`, 3), `aws/runtime` (`awscloud`, 8), `aws/core` (new: `aws_*` 7), `gcp/core` (new: `gcp_*` 6), `azure/core` (new: `azure*` 3), `inventory` (`cloudinventory` 7, `cloudasset` 3), `exposure` (`internetexposure`, 5), `multicloud` (`multicloudruntimedrift`, 3), `observability` (`obscoverage`, 11) | `aws_*` 7, `gcp_*` 6, `azure*` 3 |
| `iam/` | `can` (`iamcan`, 11), `policy` (`iampolicy`, 3), `escalation` (`iamescalation`, 6), `instanceprofile` (`iaminstprofile`, 3) | — (all four already subpackages) |
| `workload/` | `materialization` (new: `workload_materialization*` 3 + handler), `deployable` (new: `deployable_unit*` 5), `repo` (new: `repo_workload.go`) | `workload_*` ~12, `deployable_unit*` 5 |
| `repodependency/` | `repo` (new: `repo_dependency*` 8), `import` (new: `code_import*` 6, AFTER `packagecorrelation` — see note), `terraform` (`tfconfigstate` 5, `tfstate` 2), `crossrepo` (`crossrepo`, 6), `platform` (`platformfam`, 4 + `platform_infra_materialization.go` + `intent_domain_platform.go`) | `repo_dependency*` 8, `code_import*` 6 |
| `kubernetes/` | `correlation` (`kubernetescorrelation` 8 + `kubernetes_*` 3), `crossplane` (`crossplane`, 5) | `kubernetes_*` 3 |
| `security/` | `alert` (`securityalert`, 12), `group` (`secgroup`, 6), `secrets` (`secretsiam` 14 + `secrets_iam.go`), `incident` (`incident`, 8) | `secrets_iam.go` (1; the rest already subpackages) |
| `search/` | `eshu` (`eshusearch`, 9), `vector` (`searchvector`, 4), `semantic` (`semanticentity` 5 + `semantic_entity.go`) | `semantic_entity.go` |
| `decode/` | `schema` (`schemadecode`, 22), `facts` (`factdecode`, 4 + `intent_emission.go` by default, census confirms), `load` (`factload`, 3), `write` (`factwrite`, 6), `payload` (`payloadcore`, 8), `admission` (`admissiondecision`, 2) | — (`candidate_loader.go` is sole-claimed by the spine; `cloudjoin/` stays top-level, see note) |
| `intents/` | `shared` (`sharedintent`, 4), `phases` (`gpphase`, 9), `maintenance` (`maintenance`, 7), `crossscope` (`crossscope`, 4) | — (all four already subpackages) |
| `edges/` | `inheritance` (`inheritance`, 7), `sql` (`sqlrelationship`, 9), `dsl` (`dsl`, 3), `tags` (`tags`, 3), `rationale` (new: `rationale*` 3), `graph` (new: `graph_*` 4) | `rationale*` 3, `graph_*` 4 |

Notes with alternatives considered:

- `code/import` vs `repodependency/import`: `code_import_*` returns
  package-correlation types (`PackageSourceCorrelationDecision` from
  `code_import_owner_facts.go`), so it cannot move as a pure unit until
  `packagecorrelation` exists — it lands in `repodependency/import` AFTER
  `packagecorrelation`, importing it one-way. (`restructure-research.md:490`
  calls it clean; it is not — the eleventh file,
  `code_import_owner_facts.go`, is the coupling. 6 non-test files, not 11.)
- `sbomattest` lands in `supplychain/sbom`, not `security/`: SBOM
  attestation is supply-chain provenance evidence.
- `obscoverage` lands in `cloud/observability`: it correlates AWS-native
  observability objects against monitored CloudResource nodes.
- `platformfam` lands in `repodependency/platform`: Terraform runtime
  vocabulary plus the `deployment_mapping` reduction (which consumes
  `resolved_relationships` — the post-Phase-3 reopen invariant in
  `go/internal/reducer/AGENTS.md` travels with it: the move PR carries the
  existing `bootstrap-index/main.go` reopen wiring and proves it still
  fires, or the stuck-`deployment_mapping` failure mode recurs).
- `admissiondecision` lands in `decode/admission`: shared decision
  vocabulary used by correlation handlers, not one family's product.
- `crossscope` lands in `intents/crossscope`: it is read by more than one
  family (generic by the classification rule), gating cross-scope consumers
  on producer readiness.
- `cloudjoin/` stays top-level as an extracted shared-core leaf (owner
  decision on #6614): its README declares it shared-core, not a domain
  family — an identity join consumed by cloud-family packages — so nesting
  it under one family would cross the one-way-import tiers, and moving it
  under `decode/` would buy a churn PR with no navigability gain. Like the
  shared-projection tier, it is accounted for exactly where it stands; no
  move PR owns it.
- `cicdrun` lands in `supplychain/cicd`: CI-run-to-image provenance, in the
  measured cycle with `supply_chain` and `containerimage`.
- `incident` lands in `security/incident`: closest of the thirteen; it is
  response-surface correlation, not runtime machinery.
- `iaminstprofile` lands in `iam/instanceprofile`: identity-flavored
  (IAM role bound to EC2), not posture.
- `service_*` runtime core (`service.go`, `runtime.go`,
  `service_loop_config.go`, `service_runtime_instance_lookup.go`,
  `service_batch.go`, `service_heartbeat.go`, `service_observability.go`,
  `service_side_runners.go` — the `serviceSideRunner` interface plus
  `Service.startSideRunners`) STAYS in the spine: it is the
  claim/execute/ack loop, not a domain.
  The service-catalog aliases are a stanza in `compat_correlation.go` for the
  already-moved family: they burn down with the importer migration, the stanza
  does not move.
  The existing `servicecatalog/` subpackage is grandfathered interim: no
  NEW top-level package is ever created (that is what the no-55th-sibling
  rule bans), and a domain PR relocates `servicecatalog/` whole with a
  census-derived destination — see the triage roll, which names it as the
  one subpackage still awaiting a home.

## The spine (stays in root, <=40)

`registry.go` + `registry_additive_domains.go` (2; `defaults_registry.go`
counts under `defaults*`), `defaults*.go` (17), `intent.go` + `intent_value.go`
(2; `intent_emission.go` and `intent_domain_platform.go` are triage below),
`domain.go` (1), `cross_scope*.go` (2; the cross-scope readiness aliases live
as a stanza in `compat_projection.go` and burn down with the importer
migration — spine re-derives to 38 after the crossscope move), `doc.go` (1),
flat singles
`dependency.go`, `observability.go`, `partitioning.go`, `platforms.go`,
`projection.go` (5), service/runtime core (8, listed above),
`candidate_loader.go` (1): **39 files**, plus the ~4-file compat facade
(`compat_cloud.go`, `compat_correlation.go`, `compat_decode.go`,
`compat_projection.go` — the 1,593 lines of `reducer.X` aliases and
forwarders, one line per spelling, merged by the compat-consolidation PR
into 1,549 bucket lines after shared-boilerplate dedup).
Compat entries burn down to zero as the importer-migration child issue
lands; until then no new `*_compat.go`, ever — a family move adds a stanza
to the matching bucket file.

Root arithmetic after the compat-consolidation PR: 286 = 39 spine + 4 compat
facade + 243 awaiting family moves and importer migration. End state ~9:
doc.go + ~4 compat + ~4 contract surface (intent, domain, runtime,
registry). ≤40 clears with room; no exception.
`shared_projection*` (11) is NOT spine. Hoist trigger (exact): the first
domain move whose `go/types` census references a `shared_projection*`
symbol carries the hoist in the same PR; direction is family -> shared
tier, never root <- family; the shared-projection intent identity
(`shared_projection.go`) is byte-preserved and proven by B-7/B-12
like any other move. No earlier hoist (nothing needs it yet), no later
one (the first needy family cannot import root). (No line-anchored hash
cite: no SHA256 derivation lives in `shared_projection*.go` — the
`shared_projection.go:62-74` anchor from `go/internal/reducer/AGENTS.md`
points at the domain registry block, and that AGENTS.md cite is stale.)

## Triage roll (prefix-proposed, symbol-confirmed at move time)

These have a proposed home by name but were not individually measured; the
move PR's `go/types` census confirms or corrects, and this doc is amended
when it disagrees. Never a new top-level package for any of them.

| File(s) | Proposed home |
|---|---|
| `s3.go`, `ec2*` singletons | `cloud/aws/core` |
| `gcp_materialization.go` | `cloud/gcp/core` |
| `container_image.go` (singleton) | `supplychain/image` |
| `sbom*` singleton, `secrets*` singleton, `security*` singleton | `supplychain/sbom`, `security/secrets`, `security/alert` |
| `semantic*` singleton | `search/semantic` |
| `observability_coverage.go`, `quarantine*`, `decode*` (3), `shared_payload.go`, `intent_emission.go` | `decode/` children by census (`intent_emission.go` defaults to `decode/facts`) |
| `platform_infra_materialization.go` (the `platform` stanza in `compat_projection.go` burns down) | `repodependency/platform` |
| `repo_workload.go` | `workload/repo` |
| `package_publication_correlation.go`, `package_provenance_edges.go` (already inside the counted 11; no additional singletons) | `packagecorrelation/core` |
| `code_function*` (2) | `code/` child by census |
| `value_flow.go` | `code/value` |
| `workload_*` singletons (signal, identity, deployment, dependency, cloud, instance) | `workload/` children by census |
| `runs*`, `handles*`, `invokes*`, `endpoint*`, `selection*`, `shell*`, `scoped*`, `projected*`, `documentation*`, `codeowners*`, `environment*`, `symbol*`, `source*`, `materializ*`, `infrastructure*`, `cross*`, `go*`, `python*`, `parsed*` | census at move time; no placement asserted here |
| `servicecatalog/` (17, stays top-level interim) | destination by census at domain-move time — the one package still awaiting a home (`cloudjoin/` is decided top-level shared, above) |

## Sequencing (largest first, one family per PR)

1. `packagecorrelation` (12 files: the 11 `package_*` — 13 glob hits
   minus the two `supply_chain_impact_os_package_*` files — plus
   `security_alert_manifest_dependency_match.go`; needs 6 generic-func
   evictions to `payloadcore` first — `cloneBoolPointer`, `stringSet`,
   `exactManifestDependencyVersion`, `orderedStrings`,
   `packageNameFromPURL`, `packageNameFromPackageID` — measured 2026-09-08
   on #6061, no cycles after eviction). `securityAlertPackageNameMatches`
   does NOT evict to `payloadcore`: it takes
   `securityalert.ProviderSecurityAlert`, which would violate payloadcore's
   no-family-dependencies boundary (`payloadcore/README.md:17-24`). It
   travels WITH the leaf into `packagecorrelation/core`, importing the
   already-extracted `securityalert/` subpackage one-way.
2. `supplychain` core + suppression together (67 files, explicitly
   including `supply_chain_impact_finding.go`: suppression signatures take
   `SupplyChainImpactFinding`, so moving the unit while `finding.go` stays
   is a root<->package cycle — the unit is finding+core+suppression or
   nothing; `model` leaf #6568 already merged as the stated prerequisite).
3. `code/` (45 = 52 `code_call*` minus the 7 `code_call_projection*`
   runner-stays, plus 2 `code_value*` = 47; `code_import*` travels
   separately in `repodependency/import`; the runner stay set is #6609's
   list verbatim at move time — copied, never guessed; `sharedintent/doc.go`
   pins that machinery, and #6609 is OPEN and orders before this step).
4. `cloud/`, `workload/`, `repodependency/`, `intents/`, `edges/` in
   measured order; re-derive each family with `go/types` first — filename
   prefixes lie (per Lane A's query census: 4 of 46 `code*.go` files
   belonged to a different handler).
5. `iam/` (relocate the four existing subpackages under `iam/`: 11+3+6+3
   files, no root strays to adjudicate).
6. `kubernetes/` (relocate `kubernetescorrelation` + `crossplane`, absorb
   `kubernetes_*` 3).
7. `security/` (relocate `securityalert`, `secgroup`, `secretsiam`,
   `incident`; absorb `secrets_iam.go`).
8. `search/` (relocate `eshusearch`, `searchvector`, `semanticentity`;
   absorb `semantic_entity.go`).
9. `decode/` (relocate `schemadecode`, `factdecode`, `factload`,
   `factwrite`, `payloadcore`, `admissiondecision`; absorb
   `intent_emission.go` by default; `candidate_loader.go` stays spine).

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
packagecorrelation-first ordering by four days with heavy drift in the
touched root files (114 insertions / 237 deletions across 4 files vs
`origin/main`). It is superseded, not revived. The branch is left in
place for the owner to delete (destructive acts need explicit ask);
recoverable SHAs: `36ea9f98e`, `e13a30304`, base `30fcf3972`.

## DONE

Reducer root <=40 for real — no exception (decision 2 above); ~13 named
domains populated; no verb-named top-level siblings; 2b child issue filed;
final comment on #6061 with tree and counts.
