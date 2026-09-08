# Reducer target tree (#6061)

Owner-approved destination for the `go/internal/reducer` restructure.
All counts measured at `origin/main` `73ed3ad73` unless noted.

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
2. **Dirgate row: documented exception, not under-40.** Family moves alone
   cannot reach 40 (measured floor ~100-110 through moves; the spine plus
   triage roll below are structural). The row ratchets DOWN on every move PR
   and carries a one-line ledger comment naming the real target, as
   `internal/collector/gitrepo` does at 66. The ledger-comment edit lands with
   the first move PR alongside its re-pin, never alone.
3. **This tree: yes as the destination.** PR1 (this doc) commits first; no
   file moves until the doc is merged.

## The tree

Thirteen named top-level domains. Existing subpackages move UNDER a named
parent; directory paths change, Go package contents are not rewritten, and a
package keeps its name unless the verb-name itself is the readability
problem. `contract/` stays top-level (shared vocabulary, never a domain).

| Domain | Children (existing subpackage -> child) | Root buckets landing here (non-test counts) |
|---|---|---|
| `supplychain/` | `core` (new: `supply_chain_impact*` + `supply_chain_suppression*`, 67), `cicd` (`cicdrun`, 11), `image` (`containerimage`, 25), `sbom` (`sbomattest`, 7), `model` (`supplychainmodel`, 2) | `supply_chain*` 67 |
| `packagecorrelation/` | `core` (new: `package_*` 11 + `security_alert_manifest_dependency_match.go`), `source` (`packagesourcecore`, 2) | `package_*` 11, `security_alert_manifest_dependency_match.go` |
| `code/` | `call` (new: `code_call*` 52 minus the #6609 runner-stays set), `import` (new: `code_import*` 6), `intel` (`codeintel`, 5), `taint` (`codetaint`, 11), `value` (`valueflow`, 8 + `code_value*` 2) | `code_call*` 52, `code_import*` 6, `code_value*` 2 |
| `cloud/` | `aws/s3/logging` (`s3logsto`, 3), `aws/s3/grants` (`s3grant`, 3), `aws/ec2/instance` (`ec2instance`, 5), `aws/ec2/blockkms` (`ec2blockkms`, 4), `aws/ec2/usesprofile` (`ec2usesprofile`, 3), `aws/rds/posture` (`rdsposture`, 3), `aws/runtime` (`awscloud`, 8), `aws/core` (new: `aws_*` 7), `gcp/core` (new: `gcp_*` 6), `azure/core` (new: `azure*` 3), `inventory` (`cloudinventory` 7, `cloudasset` 3), `exposure` (`internetexposure`, 5), `multicloud` (`multicloudruntimedrift`, 3), `observability` (`obscoverage`, 11) | `aws_*` 7, `gcp_*` 6, `azure*` 3 |
| `iam/` | `can` (`iamcan`, 11), `policy` (`iampolicy`, 3), `escalation` (`iamescalation`, 6), `instanceprofile` (`iaminstprofile`, 3) | — (all four already subpackages) |
| `workload/` | `materialization` (new: `workload_materialization*` 3 + handler), `deployable` (new: `deployable_unit*` 5), `repo` (new: `repo_workload.go`) | `workload_*` ~12, `deployable_unit*` 5 |
| `repodependency/` | `repo` (new: `repo_dependency*` 8), `import` — see note, `terraform` (`tfconfigstate` 5, `tfstate` 2), `crossrepo` (`crossrepo`, 6), `platform` (`platformfam`, 4) | `repo_dependency*` 8 |
| `kubernetes/` | `correlation` (`kubernetescorrelation` 8 + `kubernetes_*` 3), `crossplane` (`crossplane`, 5) | `kubernetes_*` 3 |
| `security/` | `alert` (`securityalert`, 12), `group` (`secgroup`, 6), `secrets` (`secretsiam` 14 + `secrets_iam.go`), `incident` (`incident`, 8) | `secrets_iam.go` (1; the rest already subpackages) |
| `search/` | `eshu` (`eshusearch`, 9), `vector` (`searchvector`, 4), `semantic` (`semanticentity` 5 + `semantic_entity.go`) | `semantic_entity.go` |
| `decode/` | `schema` (`schemadecode`, 22), `facts` (`factdecode`, 4), `load` (`factload` 3 + `candidate_loader.go`), `write` (`factwrite`, 6), `payload` (`payloadcore`, 8), `admission` (`admissiondecision`, 2) | `candidate_loader.go` |
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
  `go/internal/reducer/AGENTS.md` travels with it).
- `admissiondecision` lands in `decode/admission`: shared decision
  vocabulary used by correlation handlers, not one family's product.
- `crossscope` lands in `intents/crossscope`: it is read by more than one
  family (generic by the classification rule), gating cross-scope consumers
  on producer readiness.
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
  `service_catalog_correlation_compat.go` is a compat forwarder for the
  already-moved family: it burns down with the 20, it does not move.
  The existing `servicecatalog/` subpackage stays top-level until a domain
  PR relocates it whole.

## The spine (stays in root, <=40)

`registry*.go` (2), `defaults*.go` (17), `intent.go` + `intent_value.go`
(2; `intent_emission.go` and `intent_domain_platform.go` are triage below),
`domain.go` (1), `cross_scope*.go` (2), `doc.go` (1), flat singles
`dependency.go`, `observability.go`, `partitioning.go`, `platforms.go`,
`projection.go` (5), service/runtime core (8, listed above),
`candidate_loader.go` (1): **39 files**. The 20 `*_compat.go`
forwarders burn down to zero as families move (no new forwarders, ever).
`shared_projection*` (11) is NOT spine: it hoists to a shared tier when its
second family consumer lands, per the seam ruling on #6061.

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
| `observability_coverage.go`, `quarantine*`, `decode*` (3), `shared_payload.go`, `intent_emission.go`, `intent_domain_platform.go` | `decode/` children by census (`intent_domain_platform.go` may adjudicate to `repodependency/platform`) |
| `platform_infra_materialization.go` (`platform_compat.go` burns down) | `repodependency/platform` |
| `repo_workload.go` | `workload/repo` |
| `package_publication.go`, `package_provenance.go` (singletons beyond the 11) | `packagecorrelation/core` |
| `code_function*` (2) | `code/` child by census |
| `value_flow.go` | `code/value` |
| `workload_*` singletons (signal, identity, deployment, dependency, cloud, instance) | `workload/` children by census |
| `runs*`, `handles*`, `invokes*`, `endpoint*`, `selection*`, `shell*`, `scoped*`, `projected*`, `documentation*`, `codeowners*`, `environment*`, `symbol*`, `source*`, `materializ*`, `infrastructure*`, `cross*`, `go*`, `python*`, `parsed*` | census at move time; no placement asserted here |
| `servicecatalog/` (17, stays top-level interim) | destination by census at domain-move time; service-catalog correlation has no domain home yet — this is the one existing subpackage without one |

## Sequencing (largest first, one family per PR)

1. `packagecorrelation` (12 files; needs 5 generic-func evictions to
   `payloadcore` first — `cloneBoolPointer`, `stringSet`,
   `exactManifestDependencyVersion`, `orderedStrings`,
   `securityAlertPackageNameMatches` — measured 2026-09-08 on #6061, no
   cycles after eviction).
2. `supplychain` core + suppression together (67 files; `model` leaf
   #6568 already merged as the stated prerequisite).
3. `code/` (52+6+2; the `code_call_projection` runner stays root per #6609,
   which is OPEN and orders before this step; `sharedintent/doc.go` pins
   that machinery).
4. `cloud/`, `workload/`, `repodependency/`, `intents/`, `edges/` in
   measured order; re-derive each family with `go/types` first — filename
   prefixes lie (per Lane A's query census: 4 of 46 `code*.go` files
   belonged to a different handler).

Preconditions per move PR: all-lanes-quiet window (re-check open PRs every
preflight); `internal/query` untouched (lanes A/B, #5167); one-way imports
only (root -> family -> shared tier -> `contract`; families never import
root); gate/spec lockstep in the same PR.

## Proof bar (every move PR, no exceptions)

- Moves are `git mv` so history follows; behavior-preserving, never mixed
  with logic changes.
- `go build ./...` + `go vet ./...`; `go test ./internal/reducer/...`
  `-count=1` recursive (never a bare package); repoint proof via
  `go test -list` (a stale `-run` selects 0 tests yet exits 0).
- Golden corpus (B-7) and e2e snapshot (B-12) byte-identical or STOP.
- Every new directory carries `doc.go`, `README.md`, `AGENTS.md` with real
  content.
- `ci-gates validate --drift` passes; telemetry-coverage row check passes.
- Dirgate row re-pinned DOWN in the same PR (see restack rule below).
- `eshu-code-review` P0=P1=P2-blocking=0 on both sides of the promotion
  preflight; only the coordinator runs `make pre-pr`, once per push.

## Restack rule (the dirgate ledger trap)

The ledger (`scripts/lib/dirgate-grandfather.tsv`, row 88:
`internal/reducer 304 1484cb0d...`) and its generated `.go` mirror
(`scripts/test-generate-dirgate-grandfather-go.sh`) conflict on every
sibling merge, and a clean merge is the dangerous case. Every move PR:
take `origin/main`'s copy of both, re-derive count and digest for the real
tree, regenerate the mirror, check the sums. Never carry a ledger resolution
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

Reducer root <=40, or the ratcheted row plus this doc as the accepted
exception; ~13 named domains populated; no verb-named top-level siblings;
final comment on #6061 with tree and counts.
