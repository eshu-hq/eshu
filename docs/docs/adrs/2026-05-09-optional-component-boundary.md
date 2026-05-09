# ADR: Optional Component Boundary For Non-Git Collectors

**Date:** 2026-05-09
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-20-terraform-state-collector.md`
- `2026-04-20-aws-cloud-scanner-collector.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `docs/docs/reference/fact-envelope-reference.md`
- `docs/docs/reference/plugin-trust-model.md`
- `docs/docs/guides/collector-authoring.md`
- GitHub issue: https://github.com/eshu-hq/eshu/issues/59

---

## Status

Accepted. This ADR is the v0.0.3 architecture gate for optional non-Git
collector boundaries before Terraform state and AWS runtime work hardens.

Accepted on 2026-05-09 after platform-owner review in GitHub issue #59. The
accepted boundary is intentionally narrow: Git remains the built-in default,
Terraform state and AWS may incubate as first-party optional component
candidates, and the full package manager remains separate follow-up work.

## Context

Eshu is moving from a Git-first pipeline toward multi-source ingestion. Git is
the default source because every Eshu installation needs repository context.
Terraform state and AWS are the next architecture gates because they prove
the code-to-cloud contract: declared infrastructure, applied infrastructure,
and observed cloud resources must converge through versioned facts and
reducer-owned graph projection.

The current implementation already reserves first-party identities for
`git`, `terraform_state`, `aws`, and `webhook` in `scope.CollectorKind`.
The workflow package also has a hard-coded `collectorContracts` registry that
maps each collector kind to reducer-owned keyspaces and required phases.
Those choices are useful for freezing the v0.0.3 contracts, but they are not a
long-term extension model.

The published extension contract is stricter than "add code wherever it fits":

- collectors observe source truth and emit versioned facts
- reducers and graph writers own canonical graph truth
- unknown or incompatible facts fail closed
- plugin and component activation requires explicit operator trust and
  configuration
- DDL and durable runtime contracts stay core-owned

That creates a product question before non-Git collectors harden: should new
collectors live in the Eshu repository?

The answer must account for different company shapes. Some companies need Git
plus AWS and Terraform state. Others run GCP, Azure, Kubernetes-only, Pulumi,
GitLab, Datadog, Snowflake, or an internal platform API. Shipping every source
family as always-on core would make the default runtime larger, harder to
operate, harder to secure, and less relevant to many users.

## Decision

Git remains built-in core. It is the default collector family and may be
enabled by normal Eshu installation paths without a component manager.

Terraform state and AWS may incubate as first-party code in the monorepo now,
but only as optional, explicitly configured component candidates. Incubation
in core is a contract-proving tactic, not a permanent ownership decision.
They must not become implicit default collectors, mandatory deployments, or
unconditional runtime dependencies.

Future collector families should generally be optional components. A new
collector may land in the Eshu repository only when one of these conditions is
true:

1. it is a core platform primitive that nearly every Eshu installation needs;
2. it is a first-party incubating component used to prove or stabilize a shared
   contract before extraction; or
3. it owns a core compatibility surface, such as fact envelopes, reducer
   contracts, trust policy, or conformance tests.

Otherwise, new collectors should target the component/plugin boundary instead
of permanently expanding core.

### Component boundary

An optional component is a source-family package with an activation boundary,
not necessarily a separate process on day one. It must declare:

- component ID and publisher identity
- collector kind or future component kind
- emitted fact kinds and schema versions
- compatible Eshu core range
- reducer or query consumer contract it requires
- activation mode and trust requirements
- operator configuration schema
- telemetry, status, and failure semantics

The component boundary is the durable design target. The initial monorepo
implementation may use Go packages, checked-in manifests, and hard-coded
registry entries while the package manager does not exist. Those temporary
mechanics must preserve the future extraction path.

### Built-in versus optional

| Family | Boundary | Default behavior | Rationale |
| --- | --- | --- | --- |
| Git | Built-in core | Enabled by default where repository sync is configured | Common denominator for code intelligence and repository truth. |
| Terraform state | First-party optional component candidate | Disabled unless explicitly configured | High-value code-to-cloud proof, but many companies do not use Terraform or cannot grant state access. |
| AWS | First-party optional component candidate | Disabled unless explicitly configured | High-value cloud observation proof, but many companies do not run AWS or cannot grant cloud inventory access. |
| Webhook/event sources | Optional component candidate | Disabled unless explicitly configured | Useful freshness path, but source-specific and not universal. |
| Future cloud, SaaS, data, or internal platform collectors | Optional components by default | Disabled unless installed and configured | Avoids permanent core bloat and keeps security posture explicit. |

### Evolution of current hard-coded registries

The existing `scope.CollectorKind` enum and workflow `collectorContracts` map
remain acceptable for the first-party bridge. They freeze the v0.0.3
tfstate/AWS contract before implementation hardens.

They should evolve in phases:

1. Keep hard-coded first-party entries for Git, Terraform state, AWS, and
   webhook while the collector contracts are still being proven.
2. Add a structured component manifest format that can describe the same
   collector kind, fact schemas, required reducer phases, and activation mode.
3. Make workflow contract loading manifest-backed for optional components while
   preserving compile-time defaults for built-in Git.
4. Move extracted optional components to first-party OCI artifacts or external
   repositories once package manager, trust verification, and conformance gates
   exist.

The long-term shape is not "everything dynamic." Core may keep a small
compile-time registry for built-in primitives. Optional collectors should be
registered through signed, allowlisted component metadata.

### No package manager in this ADR

This ADR does not implement the package manager, component installer, OCI
loader, or dynamic Go plugin system. That work belongs to the later package
manager effort.

This ADR only sets the architectural boundary so tfstate and AWS do not harden
as unavoidable core dependencies before the package manager exists.

## Consequences

### Positive

- Eshu can prove tfstate and AWS contracts now without pretending every user
  needs them.
- Operators keep explicit control over security-sensitive sources such as
  Terraform state and cloud inventory.
- Future collectors have a clear route that does not require permanent core
  expansion.
- The fact envelope, plugin trust model, and reducer phase contract remain the
  shared compatibility surface.
- The v0.0.3 work can proceed without blocking on the full package manager.

### Negative

- The monorepo will temporarily contain first-party optional collector code
  that is not yet separately packaged.
- The hard-coded `CollectorKind` and `collectorContracts` registries remain
  technical debt until manifest-backed registration lands.
- First-party incubation can blur ownership unless each collector keeps
  extraction criteria visible.
- Tests and documentation must prove disabled-by-default behavior, not just
  happy-path ingestion.

### Neutral trade-offs

- Some components may always remain first-party because they prove core
  contracts or are broadly useful. First-party does not mean always enabled.
- Optional components can share the reducer binary at first. A component
  boundary is about activation, compatibility, trust, and ownership before it
  is about process topology.

## Implementation Phases

### Phase 0: ADR gate

- Record this decision before Terraform state and AWS runtime work hardens.
- Keep the write scope to docs only.
- Do not change collector runtime code in this phase.

### Phase 1: First-party incubation

- Implement Terraform state and AWS as first-party optional component
  candidates in the monorepo.
- Keep both disabled unless explicitly configured through collector instance
  configuration.
- Reuse the shared fact envelope, workflow claims, reducer phase contracts,
  telemetry, and failure model.
- Do not add unconditional cloud SDK clients, credential loading, state readers,
  migrations, or runtime startup paths to default Git-only operation.

### Phase 2: Manifest contract

- Define a checked-in manifest shape for first-party optional components.
- Express emitted fact kinds, schema versions, reducer phase requirements,
  operator config, activation mode, and trust metadata in that manifest.
- Make the manifest redundant with the current hard-coded registry at first,
  then add tests that fail when they drift.

### Phase 3: Registry extraction

- Move optional collector contract loading from hard-coded maps toward
  manifest-backed registration.
- Keep Git as a built-in default.
- Require unknown component kinds, unknown fact kinds, unsupported schema
  versions, and missing consumer contracts to fail closed.
- Preserve deterministic workflow completeness: every optional component must
  publish the same reducer phase requirements it declares.

### Phase 4: Package manager and distribution

- Move mature optional components to first-party OCI artifacts or external
  repositories when the package manager exists.
- Enforce signature/provenance checks, allowlists, revocation, and compatible
  core ranges before activation.
- Keep component extraction reversible until conformance tests prove parity
  with the incubated monorepo implementation.

## Acceptance Criteria

- The default Git-only runtime does not require Terraform state, AWS, or any
  future optional collector configuration.
- Terraform state and AWS collectors, when implemented, are disabled unless
  an operator explicitly declares collector instances and required credentials
  or state sources.
- Optional collectors emit versioned facts and never write canonical graph
  rows directly.
- Optional collectors declare downstream reducer or query consumer contracts
  before their facts can become active platform truth.
- The current hard-coded `CollectorKind` and `collectorContracts` bridge is
  documented as temporary for optional collectors, with Git allowed to remain
  built-in.
- Adding a new non-Git collector requires either an ADR exception for monorepo
  incubation or a component manifest path.
- Unknown, untrusted, disabled, or schema-incompatible components fail closed
  with operator-visible status and logs.
- Test plans for tfstate/AWS include disabled-by-default behavior, activation
  configuration, fact schema compatibility, reducer phase publication, and
  telemetry verification.

## Rejected Alternatives

### Put every first-party collector permanently in core

Rejected. This would make every installation carry unused dependencies,
credentials, configuration, test burden, and security surface. It also assumes
all companies need the same source systems, which is false.

### Block tfstate and AWS until the package manager exists

Rejected. Terraform state and AWS are needed to prove the multi-source
contracts now. Waiting would delay the v0.0.3 architecture gate and leave the
fact, reducer, and workflow contracts under-tested.

### Make every collector external immediately

Rejected. The boundary is not mature enough. Terraform state and AWS need
first-party incubation to prove scope identity, generation identity, reducer
phases, cross-source readiness, and operator telemetry before extraction.

### Keep dynamic plugins but skip trust and schema gates

Rejected. Optional components observe sensitive systems and can emit facts that
influence canonical graph truth. They must follow the plugin trust model, fact
schema versioning policy, and fail-closed behavior.

## Unresolved Questions

- What is the exact component manifest schema and file location?
- Which package manager issue owns OCI resolution, installation, upgrades, and
  rollback behavior?
- Which first-party optional components should remain in the monorepo after
  the package manager ships?
- Should optional component manifests be loaded by the coordinator, reducer,
  runtime wiring, or a shared registry package?
- What is the minimum conformance suite required before extracting tfstate or
  AWS out of core?
