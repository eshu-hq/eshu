# Naming remediation: coordinator, projector, mcp

## Why

`internal/coordinator`, `internal/projector` and `internal/mcp` finished their
restructure before [Naming](../naming.md) existed. All three closed with zero
nesting and a flat row of siblings whose names glue two or three full words
together — `observabilitycoveragematerialization`, `awscloudruntimedrift`,
`supplychainevidence`, and eleven coordinator directories that all end in
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

Every one of these packages is a row of very small siblings, not a few large
ones. Coordinator's nineteen directories hold exactly two non-test files each.
Projector and mcp are mostly two or three, with a handful at four to eight. So
the work is grouping tiny packages under a readable parent, not decomposing
large ones.

| package | root non-test | subdirs | nested today |
| --- | ---: | ---: | ---: |
| `internal/coordinator` | 49 | 19 | 0 |
| `internal/projector` | 47 | 32 | 0 |
| `internal/mcp` | 105 | 36 | 0 |

## coordinator

Eleven of nineteen names end in `planner`. The package is telling you what its
parent should be. Reading `prometheusmimir` shows that it belongs under the
same parent: it plans metric-metadata collection for Prometheus and Mimir
targets through one implementation.

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
│   ├── metrics/                   ← prometheusmimir
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

```
projector/
├── aws/
│   ├── ec2/  s3/  rds/            ← already clean, relocated under aws
│   ├── relationship/              ← awsrelationship
│   ├── resource/                  ← awsresource
│   └── cloud/image/               ← awscloudimage
├── cloud/
│   ├── inventory/                 ← cloudinventory
│   └── runtime/drift/
│       ├── aws/                   ← awscloudruntimedrift
│       └── multi/                 ← multicloudruntimedrift
├── code/
│   ├── function/summary/          ← codefunctionsummary
│   ├── interproc/evidence/        ← codeinterprocevidence
│   └── taint/evidence/            ← codetaintevidence
├── iam/
│   ├── trust/                     ← iamcanassume
│   └── instance/profile/          ← iaminstanceprofile
├── observability/coverage/        ← observabilitycoverage
│   └── materialization/           ← observabilitycoveragematerialization
├── container/image/identity/      ← containerimageidentity
├── cicd/run/correlation/          ← cicdruncorrelation
├── crossplane/satisfaction/       ← crossplanesatisfiedby
├── incident/routing/              ← incidentrouting
├── package/source/                ← packagesource
├── sbom/attestation/              ← sbomattestation
├── secrets/iam/                   ← secretsiam
├── semantic/entity/               ← semanticentity
├── service/catalog/               ← servicecatalog
├── supply/chain/impact/           ← supplychainimpact
├── workload/cloud/                ← workloadcloud
└── azure/  gcp/  kubernetes/  intent/  security/   ← already clean
```

Two groupings here say something the flat list hid. `awscloudruntimedrift` and
`multicloudruntimedrift` are one concept at two scopes, so they pair under
`cloud/runtime/drift/`. And `observabilitycoveragematerialization` is the
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
├── secrets/iam/                   ← secretsiam
├── security/alert/                ← securityalert
├── service/                       ← already clean, 4 files
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

- **`prometheusmimir` becomes `planner/metrics`.** It builds workflow rows for
  enabled Prometheus or Grafana Mimir metric-metadata targets. The providers
  are alternatives within one planner, not a parent and child. Keep the
  existing package intact and document its supported targets.
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
  AWS-only triggering.
- **IAM trust and Crossplane satisfaction use their package responsibilities.**
  `iamcanassume` becomes `iam/trust`, matching its decoded trust-statement
  predicate. `crossplanesatisfiedby` becomes `crossplane/satisfaction`, matching
  its Claim-satisfaction intent. The graph relationships remain `CAN_ASSUME`
  and `SATISFIED_BY`.
- **Instance profile and supply chain retain their domain meaning with readable
  paths.** Use `iam/instance/profile`, projector's `supply/chain/impact`, and
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
- **Single-child parents are accepted.** `vault/live/`, `scanner/worker/` and
  `oci/registry/` each end up as a parent holding one child. That is the point
  of rule 3: the parent is the future boundary, and the thin directory today is
  the cost of being able to add a sibling tomorrow without renaming anything.

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
runtime package boundaries stay intact. Root counts end unchanged — coordinator
49, projector 47, mcp 105 — unless a rename genuinely orphans a file, which
must be named and explained.

Every newly created directory, including each intermediate namespace parent,
must contain an accurate `README.md`, `AGENTS.md`, and `doc.go`. A namespace-only
parent's `doc.go` contains only its package documentation and a legal package
clause; it adds no imports, declarations, initialization, or runtime ownership.
Use `package packages` for directories named `package`, because `package` is a
Go keyword. These are documentation-only Go packages. Existing runtime parents
retain their implementation and have their documentation updated for the new
children. Move and update each leaf's existing documentation trio.

The MCP children's existing filenames are already clean: `doc.go`, `routes.go`,
`AGENTS.md`, and `README.md`. Their package declarations, imports, aliases, and
references still need updating. List each coordinator and projector child's
files before moving it and fix any filename stutter in the same PR.

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
3. **mcp** — blocked until #6625 merges; that PR touches one mcp file, #6619
   touches four and #6612 touches ten.

One package per PR, and within a package one parent-nest per PR.
