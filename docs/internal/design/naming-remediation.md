# Naming remediation: coordinator, projector, mcp

## Why

`internal/coordinator`, `internal/projector` and `internal/mcp` finished their
restructure before [Naming](../naming.md) existed. All three closed with zero
nesting and a flat row of siblings whose names glue two or three full words
together — `observabilitycoveragematerialization`, `awscloudruntimedrift`,
`supplychainevidence`, and ten coordinator directories whose names end in
`planner`.

Rule 3 forbids that shape, and rule 5 makes fixing it part of any move that
touches those paths. This work replaces mechanical word chains with names that
describe the code's responsibility and makes the package ownership map usable
for future service extraction. Readable paths identify potential extraction
units; imports and runtime contracts determine whether those units can move
independently. Each directory is a namespace or package boundary, not
automatically a future service or repository.

This is a rename-and-nest plan only. It does not move root files and does not
revisit what #6058 and #6057 decided stays at root — mcp's `dispatch_status.go`
spans six unrelated domains, and projector's two remaining schedulers share a
struct type that cannot cross a package boundary. Those findings stand.

## Shape of the problem

All counts in this plan describe the #6627 planning snapshot at commit
`cb032ab3ef1cf167bff2d33e08da5058b25c8004`, not a live inventory.
Before each rename PR, remeasure the affected subtree against its rebased base
and record the before/after counts. Distinguish changes from intervening merges
from changes introduced by the rename.

Every one of these packages is a row of very small siblings, not a few large
ones. Coordinator's nineteen directories hold exactly two non-test Go files
each. Projector and mcp child packages are mostly two or three non-test Go
files, with a handful at four to eight. So the work is grouping tiny packages
under a readable parent, not decomposing large ones.

| package | root non-test Go files at snapshot | child dirs at snapshot | nested at snapshot |
| --- | ---: | ---: | ---: |
| `internal/coordinator` | 49 | 19 | 0 |
| `internal/projector` | 47 | 32 | 0 |
| `internal/mcp` | 105 | 36 | 0 |

## coordinator

In the planning snapshot, ten of the nineteen coordinator child names end in
`planner`. Those ten packages belong under `planner/`. `prometheusmimir` joins
them because it plans metric-metadata collection for Prometheus and Mimir
targets through one implementation, although its current name hides that
responsibility.
`plannercontract` also moves under this parent as the shared plan-key contract,
not as another planner implementation.

Reconciliation (2026-09-16, observed on `origin/main` `8920e2183`): this
nesting has landed as `coordinator/{cicd,component,oci,planner,sbom,scanner,
securityalert,vault}` with
`planner/{aws/{freshness,scheduled},component/extension,contract,gcp,grafana,
jira,loki,metrics,pagerduty,tempo,tfstate}`, `component/activation`, and
`scanner/worker`. The one remainder is the #6692 owner decision below:
`planner/metrics` becomes `planner/prometheus` so it matches the collector at
`collector/observability/prometheus`. That rename moves with #6627, not here.
Since that observation, `securityalert` has moved to `security/alert` (#6796);
the target tree below already reflects the new path.

```
coordinator/
├── planner/
│   ├── contract/                  ← plannercontract
│   ├── aws/freshness/             ← awsfreshnessplanner
│   ├── aws/scheduled/             ← awsscheduledplanner
│   ├── component/extension/       ← componentextensionplanner
│   ├── gcp/                       ← gcpplanner
│   ├── grafana/                   ← grafanaplanner
│   ├── jira/                      ← jiraplanner
│   ├── loki/                      ← lokiplanner
│   ├── prometheus/                ← prometheusmimir (renamed from planner/metrics per #6692)
│   ├── pagerduty/                 ← pagerdutyplanner
│   ├── tempo/                     ← tempoplanner
│   └── tfstate/                   ← tfstateplanner
├── component/activation/          ← componentactivation
├── cicd/run/                      ← cicdrun
├── oci/registry/                  ← ociregistry
├── sbom/attestation/              ← sbomattestation
├── scanner/worker/                ← scannerworker
├── security/alert/                ← securityalert
└── vault/live/                    ← vaultlive
```

## projector

Reconciliation (2026-09-16, observed on `origin/main` `8920e2183`): most of
this nesting has landed. `projector/` now holds `aws/{cloud/image,ec2,rds,
relationship,resource,s3}`, `azure/`, `gcp/`,
`cloud/{inventory,runtime/drift/{aws,multi}}`, `cicd/run/`,
`code/{function,interproc,taint}`, `incident/routing/`, `intent/`,
`kubernetes/`, `package/source/`, `security/`, and `workload/cloud/`. The drift
pairing landed at `cloud/runtime/drift/{aws,multi}` and the image builder at
`aws/cloud/image/`; those as-built paths stand until #6627 decides otherwise
with source evidence. What remains glued under #6627:

```
projector/  (remaining glued packages)
├── container/image/identity/      ← containerimageidentity
├── crossplane/satisfaction/       ← crossplanesatisfiedby
├── cloud/aws/iam/trust/           ← iamcanassume (AWS-only; #6627 confirms by census)
├── cloud/aws/iam/instance/profile/ ← iaminstanceprofile (AWS-only; #6627 confirms by census)
├── observability/coverage/        ← observabilitycoverage
│   └── materialization/           ← observabilitycoveragematerialization
├── sbom/attestation/              ← sbomattestation
├── access/posture/                ← secretsiam (projection-owned; shares only the trailing segment with collector/access/posture, not its runtime or fact shapes)
├── semantic/entity/               ← semanticentity
├── service/catalog/               ← servicecatalog
└── supply/chain/impact/           ← supplychainimpact
```

The IAM destinations follow the reducer's `cloud/<provider>/` layout for
AWS-only builders; if census shows shared (multi-provider) use, they stay in a
named shared owner instead. `observabilitycoveragematerialization` is the
materialization step of `observabilitycoverage`, so it nests inside it rather
than sitting beside it.

`interproc` stays abbreviated: the short form is used throughout the reducer
and query packages, which is the exception rule 1 allows.

## mcp

```
mcp/
├── code/
│   ├── flow/                      ← codeflow
│   ├── intel/                     ← codeintel
│   ├── quality/                   ← codequality
│   ├── owners/                    ← codeowners
│   └── dead/                      ← deadcode
├── supply/chain/{evidence,impact}/ ← supplychainevidence, supplychainimpact
├── infra/{inventory,search}/      ← infrainventory, infrasearch
├── contract/{route,tool}/         ← routecontract, toolcontract
├── admission/decisions/           ← admissiondecisions
├── container/image/               ← containerimage
├── entity/resolution/             ← entityresolution
├── iac/management/                ← iacmanagement
├── observability/coverage/        ← observabilitycoverage
├── package/registry/              ← packageregistry
├── access/posture/                ← secretsiam (owner-settled; see Decisions taken)
├── alerts/                        ← securityalert (owner-settled; see Decisions taken)
├── service/                       ← already clean, 4 non-test Go files
│   └── context/                   ← servicecontext
└── ask/ cicd/ cloud/ content/ documentation/ ecosystem/ freshness/ impact/
    investigation/ kubernetes/ playbooks/ relationships/ replatforming/
    semantic/ visualization/       ← already clean
```

`deadcode` becomes `code/dead`, not `dead/code`. There is already a `code/`
parent, so dead-code analysis belongs inside it; a mechanical de-glue would
have invented a `dead/` top level that means nothing. Grouping beats splitting
when a parent already exists.

`codeowners` becomes `code/owners`. The package still owns CODEOWNERS file
semantics; the directory spelling does not change that format or its ownership.

## Decisions taken

- **MCP secrets/IAM lives at `access/posture`, not `secrets/iam`.** The
  family owns identity trust chains and privilege posture observations, not
  secrets alone, so the posture grouping names the content better than the
  secret store it reads. The route-selection nest was created at
  `access/posture` per the #6692 owner decision (commit `22ba4c5d`), and
  #6816 later moved the tool definitions into that existing nest instead
  of inventing a parallel one. Owner decision.
- **MCP security-alert family lives at `alerts/`, not `security/alert`.**
  One plain word, unambiguous inside `internal/mcp` (the only alert
  family), with package `alerttools` matching the sibling pattern
  (`codeflowtools`, `secretsiamtools`). Owner decision; recorded after a
  review P2 on #6823 proposed the nested path.
- **`prometheusmimir` becomes `planner/prometheus`.** It builds workflow rows for
  enabled Prometheus or Grafana Mimir metric-metadata targets. The providers
  are alternatives within one planner, not a parent and child. Keep the
  existing package intact and document its supported targets.
  Mimir is reached through the Prometheus API plus tenant header, so the path
  names the protocol (see #6692 owner decision 1). The rename itself moves
  with #6627.
  `PlanPrometheusMimirWork`, the collector kind, provider values, identities,
  and runtime behavior remain unchanged.
- **Component activation remains a shared contract outside the planner
  subtree.** `planner/component/extension` continues to use
  `component/activation` for configuration parsing and values. Its five
  production consumers are configuration construction
  (`component_activation_config.go`), component-extension scheduling
  eligibility and egress-policy checks (`component_extension_service.go`),
  workflow planning (`componentextensionplanner/planner.go`), governance audit
  (`governance_audit.go`), and PagerDuty exclusion (`pagerduty_service.go`).
  Preserve and document these dependencies. Activation must not import the
  planner or coordinator root. Moving its path must update every current
  consumer in the same PR; do not relocate the contract into the planner or
  duplicate it there.
- **Projector's `code/*` splits to three levels** — `code/function/summary`
  rather than `code/functionsummary`. Consistent with `container/image/identity`
  and `cicd/run/correlation` in the same package.
- **Runtime drift splits** to `cloud/runtime/drift/{aws,multi}`. The two
  existing packages remain separate: the AWS builder triggers on AWS resource
  facts; the multi builder triggers on GCP/Azure facts and deliberately excludes
  AWS-only triggering. This has landed in the projector as-built; the reducer's
  `multicloudruntimedrift/` follows under #6061.
- **IAM trust and Crossplane satisfaction use their package responsibilities.**
  `iamcanassume` becomes `cloud/aws/iam/trust` when census confirms it is
  AWS-only, matching its decoded trust-statement predicate — the
  `cloud/<provider>/` direction from #6061. `crossplanesatisfiedby` becomes
  `crossplane/satisfaction`, matching its Claim-satisfaction intent. The graph
  relationships remain `CAN_ASSUME` and `SATISFIED_BY`.
- **Instance profile and supply chain retain their domain meaning with readable
  paths.** Use `cloud/aws/iam/instance/profile` when census confirms AWS-only
  use, projector's `supply/chain/impact`, and
  MCP's `supply/chain/{evidence,impact}`. No fused-name exception is needed.
  Preserve the existing package boundaries; MCP evidence and impact remain
  siblings.
- **Package-qualified stutter is removed; planner method contracts remain
  stable.** The AWS drift, multi-cloud drift, IAM instance-profile, and
  supply-chain-impact builders become `BuildReducerIntent` in their respective
  leaf packages. Existing coordinator methods such as `PlanGCPWork` remain
  unchanged: production invokes them on planner values through
  root-owned interfaces, rather than as `gcp.PlanGCPWork`. `gcp.WorkPlanner`
  and `gcp.PlanRequest` do not repeat the leaf package name.
- **Single-child parents are accepted.** `vault/live/`, `scanner/worker/`, and
  `oci/registry/` each initially contain one child. The parent is a stable
  namespace for related siblings and a candidate ownership seam; independent
  extraction still depends on import direction and runtime contracts.

## Scope and constraints

Rename and nest directories, destutter files that repeat their directory name
per rule 2, and update package declarations, imports, aliases, and every path
reference. Internal package-level Go identifiers may be shortened where the new
package name supplies repeated context. Update their callers and documentation
in the same PR. Preserve receiver method names and method sets, including the
coordinator's existing `Plan…Work` interface methods; this plan does not unify
or redesign planner interfaces.

Route and tool registrations, wire names and payloads, reducer domains and
entity keys, runtime behavior, ordering, and telemetry stay identical. Existing
runtime package boundaries stay intact. Root runtime-file counts must remain
unchanged relative to each rename PR's rebased base, unless the rename genuinely
orphans a file, which must be named and explained.

Every newly created directory, including each intermediate namespace parent,
must contain an accurate `README.md`, `AGENTS.md`, and `doc.go`. A namespace-only
parent's `doc.go` contains only its package documentation and a legal package
clause; it adds no imports, declarations, initialization, or runtime ownership.
Use `package packages` for directories named `package`, because `package` is a
Go keyword. These are documentation-only Go packages. Existing runtime parents
retain their implementation and have their documentation updated for the new
children. Move and update each leaf's existing documentation trio.

MCP children already use clean filenames such as `doc.go`, `routes.go`,
`tools.go`, `contract.go`, `AGENTS.md`, and `README.md`; the exact mix varies by
child. Their package declarations, imports, aliases, and references still need
updating. List every coordinator, projector, and MCP child's files before
moving it and fix any filename stutter in the same PR.

Prepare for future service repositories by preserving existing ownership and
making dependencies explicit. This issue does not split services, change Go
modules, introduce shared SDKs, move runtime responsibilities, or replace
in-process calls with network calls. Existing cross-package dependencies remain
in place and are recorded where they would matter to a later extraction. Do not
describe a renamed subtree as independently buildable unless that has been
separately established.

Keep implementation under its existing owner even when another service has a
similarly named domain. Coordinator planners remain coordinator-owned;
projector builders remain projection-owned; MCP registration and route
selection remain MCP-owned. Namespace parents contain documentation only.
Shared contract packages retain their current owners and dependency direction.

The original names came from directory listings and file counts. The
package-grounded decisions above supersede those original spellings. Read each
remaining package before its move; raise any further naming disagreement on
#6627 with source evidence.

## Per-PR acceptance

For every parent-nest PR:

- Explain each destination's responsibility in plain English and identify its
  runtime owner or consumers. Review package-qualified identifiers at real call
  sites; preserve receiver methods and interface contracts where the move
  creates no package-name stutter.
- Record the moved packages' direct imports and reverse importers before and
  after the move. After substituting the old-to-new path map, the dependency
  edges must remain equivalent, apart from new documentation-only namespace
  packages. Include production and test imports.
- Preserve root-to-child orchestration. A moved child must not acquire an import
  of its orchestrating root; a shared contract must not acquire an import of its
  consumers. Detect new sibling dependencies, cross-service dependencies, and
  imports of concrete storage or transport implementations.
- Move a shared contract and update all its consumers together. Do not leave an
  old-path forwarding package, duplicate contract types, or copy implementation
  to make a staged rename compile. Preserve existing type-alias relationships.
- Update package documentation with the owner, callers, shared contracts, and
  dependencies outside the subtree that a future extraction would need to
  address. Record existing coupling without removing it in this issue.
- Complete each PR with valid imports and existing behavior independently of
  later PRs. If completing a move requires changing ownership or touching
  another protected lane, report the exact dependency on #6627 before
  proceeding.

## Order

1. **coordinator** — clearest win, establishes the pattern.
2. **projector** — worst offenders, most judgement.
3. **mcp** — start after #6625's code-family move merges. Treat #6619 and #6612
   as live overlap dependencies: inspect their current diffs and merge state
   before selecting an MCP parent-nest, and recheck all three PRs and other
   overlapping work at every preflight. Include merged dependencies in the
   working base. Begin an overlapping rename only after its dependency has
   landed or the ownership and scope have been resolved on #6627.

Before moving `mcp/code/*`, revalidate the proposed grouping against the merged
result of #6625, which moves query execution into `internal/query/codequery`
with analysis behind its `deadcode` leaf. Inspect the current MCP family
responsibilities, registration and dispatch boundaries, imports, shared
contracts, and handler-path references. Preserve the separation between MCP
registration and request selection and query execution; matching domain names
do not make them one owner or a shared package. If the merged code contradicts
a proposed MCP destination, update this plan and #6627 with source evidence
before the move. Changes to the query lane remain outside this issue.

One package per PR, and within a package one parent-nest per PR.
