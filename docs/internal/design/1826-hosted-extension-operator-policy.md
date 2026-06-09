# Hosted Extension Operator Policy (#1826)

Status: **PROPOSED - SECURITY REVIEW REQUIRED BEFORE IMPLEMENTATION.**

Refs #1826. Refs #1817, #1819, #1820, #1821, #1825, #1830, #1922,
#1923. See also [System Architecture](../../public/architecture.md),
[Component Package Manager](../../public/reference/component-package-manager.md),
[Plugin Trust Model](../../public/reference/plugin-trust-model.md),
[Collector Extension SDK Contracts](1821-collector-extension-sdk-contracts.md),
[Community Extension Index And Publication Workflow](1830-community-extension-index-publication-workflow.md),
and [Hosted Extension Operator Policy](../../public/operate/hosted-extension-policy.md).

This is a maintainer-only internal design doc. It is not part of the public
MkDocs site (`docs_dir: public`), so the public docs build does not validate
these links.

## Decision

Hosted community extension execution needs a core-owned operator policy gate
between local component activation and workflow-coordinator claims. The policy
must be explicit, fail closed, auditable, and separate these states:

| State | Meaning | Authority |
| --- | --- | --- |
| Installed | A verified manifest is recorded in component state. | Local component manager. |
| Enabled | An operator created a named activation with bounded config handles. | Component manager plus hosted policy source. |
| Claim-capable | The hosted coordinator may create work for that activation. | Hosted operator policy and workflow coordinator. |

Installed is not enabled. Enabled is not claim-capable. Claim-capable is
revocable independently of package installation so operators can stop new work
without deleting local package state or losing audit history.

The v1 implementation should model the policy as a declarative document or
values block loaded by the component manager, extension host, and workflow
coordinator. Central policy distribution may come later, but the initial source
must still have a stable digest/version so status, audit rows, and claim
decisions can name the policy revision without exposing private config.

## Non-Goals

- This design does not implement the SDK host, Helm values, Compose env wiring,
  API/MCP diagnostics, or workflow-coordinator bridge.
- It does not grant trust from community index membership alone.
- It does not make shared hosted API tokens tenant-isolated.
- It does not allow extensions to import Eshu internals, open Postgres or graph
  handles, run DDL, or write graph truth.
- It does not permit raw credentials in manifests, facts, logs, metrics, issue
  bodies, docs, or policy examples.

## Policy Evaluation Order

Every install, enable, and claim decision must evaluate the same ordered gates
and stop at the first terminal denial:

1. **Revocation gate.** Revoked component ID, publisher, artifact digest,
   version range, or policy revision wins over all allowlists and prior state.
2. **Trust gate.** `disabled` denies all optional components; `allowlist`
   requires exact component and publisher approval; `strict` also requires
   provenance verification from #1819.
3. **Compatibility gate.** `compatibleCore`, SDK protocol, declared fact
   families, reducer consumer contracts, and telemetry prefix must be accepted
   by the running core.
4. **Enablement gate.** A named instance must be enabled for a bounded scope
   with config handles, not raw config bodies.
5. **Credential-reference gate.** Required credentials must be present as
   references and resolvable by the runtime identity, never persisted as values.
6. **Isolation gate.** Runtime image, service account, resource budget, network
   egress, filesystem posture, and process/container boundary must match an
   approved isolation profile.
7. **Claim-capable gate.** The workflow coordinator may plan work only after
   the enabled activation, trust decision, isolation profile, credential
   references, tenant or workspace scope, and policy revision all match.

Allowed decisions should name the policy revision, component ID, instance ID,
collector kind, scope class, trust mode, and isolation profile. Denied decisions
must use bounded reason classes such as `revoked_policy`, `disabled_policy`,
`untrusted_publisher`, `provenance_required`, `incompatible_core`,
`invalid_config`, `credential_reference_missing`, `isolation_profile_denied`,
`tenant_scope_denied`, or `claim_capability_denied`.

## Policy Shape

The stable schema should keep private values out of the document:

```yaml
apiVersion: eshu.dev/v1alpha1
kind: HostedExtensionPolicy
metadata:
  policyId: hosted-extensions-default
  revision: "2026-06-09.1"
spec:
  trustMode: allowlist
  allow:
    components:
      - id: dev.example.collector.scorecard
        publisher: example
        versions: ">=0.1.0 <0.2.0"
  revoke:
    components: []
    publishers: []
    digests: []
  instances:
    - instanceId: scorecard-team-a
      componentId: dev.example.collector.scorecard
      enabled: true
      claimCapable: false
      collectorKind: scorecard
      scopes:
        - kind: repository
          selector: team-a/*
      credentials:
        - name: scorecard_api
          kind: environment_variable
          handle: SCORECARD_API_TOKEN
      isolationProfile: hosted-extension-restricted
      tenantScope:
        mode: single_tenant
        workspace: team-a
```

`claimCapable: false` is the safe default. A future implementation may split
the policy into operator values, admission rows, and status rows, but it must
preserve the same redaction and decision contract.

## Install, Enable, And Claim-Capable Semantics

| Operation | Required checks | Result |
| --- | --- | --- |
| Install | Manifest validity, trust mode, allowlist, revocation, compatible core. | Copies or records the manifest only. No runtime work. |
| Enable | Installed package, enabled instance ID, bounded config handles, credential references, allowed source scope. | Records activation. No workflow work unless claim-capable is also approved. |
| Claim-capable | Enabled activation plus hosted policy, isolation profile, credential-reference resolution, workflow-coordinator active mode, and not revoked or incompatible. | Coordinator may create work and the extension host may launch the extension. |

The workflow coordinator should treat `component_id`, `instance_id`,
`collector_kind`, `scope_kind`, `scope_id`, `policy_revision`, and
`artifact_digest` as the claim-capable identity. This gives retries and
duplicate coordinator reconciliation a stable idempotency key while preserving
the ability to stop one instance without stopping every instance of a package.

## Revocation

Revocation must be safe under retries and in-flight work:

- Revocation wins over allowlists, index membership, prior verification,
  installation, and activation.
- The coordinator must stop creating new work for revoked identities as soon as
  the new policy revision is observed.
- Pending work for revoked identities becomes ineligible and must surface
  `revoked_policy` or a more specific bounded reason.
- The extension host must re-check the policy before launching a claim and
  before committing facts. If the policy became revoked before commit, the host
  must reject the commit and fail the claim terminal with a bounded reason.
- For compromise-class revocations, operators should scale down or terminate
  the extension workload and let existing claim leases expire or be reaped
  according to the normal workflow contract.
- Status and diagnostics must show component ID, instance ID, reason class,
  policy revision, and effective time. They must not show private scopes,
  credential handles when policy forbids them, source payloads, or secrets.

Revocation cannot undo a network read that already happened before the runtime
observed the new policy. The commit gate is therefore mandatory: revoked work
must not produce new facts or graph-visible truth after the revocation
effective time.

## Credential References

Hosted extension policy may reference only credential handles:

| Kind | Allowed handle | Notes |
| --- | --- | --- |
| `environment_variable` | Env var name such as `SCORECARD_API_TOKEN`. | Value is injected by local shell, Compose env file, or Kubernetes Secret. |
| `kubernetes_secret_ref` | Secret name plus key. | Stored in private values or GitOps secret management, not public docs. |
| `external_secret_ref` | Vault, External Secrets, or cloud secret manager handle. | Runtime receives a resolved environment variable or workload identity token. |
| `workload_identity` | Service account, role binding, or provider identity handle. | Preferred for cloud APIs when available. |
| `local_dev_profile` | Local profile name. | Local experimentation only, never hosted proof. |

Raw credential values, bearer tokens, API keys, private keys, provider request
ids, signed URLs, token-bearing URLs, and credential-bearing config bodies must
not be written to facts, component manifests, policy docs, logs, metrics,
status payloads, issue bodies, or PR descriptions. Metric labels should use
bounded credential-source classes, not handle names.

## Isolation Profile

The hosted isolation profile is part of the claim-capable decision, not an
implementation detail.

Required controls:

- separate process or container boundary for out-of-tree extension code;
- no direct Postgres, graph, reducer, API, MCP, or workflow store handles;
- read-only root filesystem where the platform supports it;
- non-root user, dropped Linux capabilities, and runtime-default seccomp for
  Kubernetes;
- explicit CPU, memory, timeout, max input, and max fact limits;
- egress allowlist or deny-by-default NetworkPolicy for Kubernetes;
- service account or workload identity scoped to the extension's source system;
- config mounted as handles and safe metadata, not raw secrets;
- `/healthz`, `/readyz`, `/metrics`, and `/admin/status` for hosted runtimes;
- policy revision, instance ID, collector kind, and safe scope class in status.

Local mode may relax process/container isolation for development, but it must
not be described as hosted proof. Docker Compose may prove a representative
runtime profile before Kubernetes rollout, but remote Compose proof is still
required before a public hosted deployability claim.

## Tenant And Workspace Scope

Policy should include a tenant or workspace scope even before Eshu has complete
per-tenant auth. The scope is an authorization and audit boundary for planning
work, not a promise that shared API tokens isolate reads.

Until per-team tokens and hosted ACLs are implemented, hosted docs must keep
stating that the deployed shared bearer token is not a per-team or
per-repository isolation boundary. Extension policy can limit what an extension
collects; it does not limit what an API token holder can read from already
indexed data.

## Threat Model

### Assets

- operator allowlists, revocation entries, and hosted policy revisions;
- component manifests, artifact digests, publisher identities, and signatures;
- extension config handles and credential references;
- workflow claims, claim leases, fencing tokens, and retry/dead-letter state;
- source facts, reducer-owned graph truth, and query/read-model answers;
- service accounts, network egress rules, resource budgets, and audit/status
  evidence.

### Trust Boundaries

| Boundary | Risk | Required control |
| --- | --- | --- |
| Community manifest to local install | Typosquatting or unsafe fact claims | Manifest validation, allowlist, compatible-core, fact-kind ownership |
| Local enablement to hosted policy | Local activation implies hosted work | Separate claim-capable gate and hosted policy revision |
| Hosted policy to workflow coordinator | Revoked or disabled instance gets work | Revocation-first evaluation and idempotent claim-capable identity |
| Coordinator claim to extension host | Stale or mismatched claim is launched | Fencing token, policy re-check, scope and generation validation |
| Extension process to fact commit | Extension emits undeclared or sensitive facts | Core-owned validation and redaction before commit |
| Credential reference to provider read | Secret value leaks or wrong credential used | Reference-only config, runtime identity, no secret persistence |
| Extension runtime to network | Over-broad egress or data exfiltration | Egress allowlist, service-account boundary, provider-specific scopes |
| Status and telemetry to operators | Private config leaks through diagnostics | Low-cardinality reason classes and safe handles only |

### Attacker Capabilities

The design assumes an attacker may publish a lookalike package, compromise a
publisher account, submit a malicious manifest, exploit an extension runtime,
try to reuse stale claims after revocation, request broader source scopes, or
induce failures that leak private configuration through logs and metrics.

The design does not assume arbitrary community code is safe because it is
installed, indexed, or signed.

### Abuse Paths And Mitigations

| Abuse path | Impact | Mitigation |
| --- | --- | --- |
| Installed package starts collecting without operator approval | Unapproved source reads | Enablement and claim-capable states are separate and default off |
| Revoked extension receives retry work | Known-bad code continues to run | Policy revision checked before claim creation, launch, and commit |
| Extension emits undeclared core fact kinds | Graph/query truth corruption | Manifest fact-kind ownership and core validation before commit |
| Extension reads with raw embedded credential | Secret leakage in config and diagnostics | Credential references only; raw values rejected or never accepted |
| Extension escapes intended provider scope | Cross-team or cross-tenant collection | Scope selectors, service account boundary, and egress allowlist |
| Compromised extension exfiltrates data over network | Data loss | Deny-by-default egress and narrow provider destinations |
| Failure messages expose private config | Secret or tenant data leak | Bounded failure classes, fingerprints, and status redaction |
| In-flight claim commits after policy revocation | Facts persist after stop signal | Commit-time policy check and terminal failure on stale policy |

## Concurrency And Retry Contract

Workflow stages:

```text
policy source -> component activation -> coordinator reconciliation
  -> claim creation -> extension-host launch -> fact validation and commit
  -> claim complete/retry/terminal/dead-letter -> status and diagnostics
```

Shared state and conflict domains:

- policy revision for component ID, publisher, digest, and instance ID;
- activation rows for `(component_id, instance_id)`;
- workflow work and claim rows keyed by collector kind, scope, instance, and
  policy revision;
- fact stable keys inside the claimed scope and generation.

Transaction scope should cover claim mutation and fact commit under the active
fence. Retry scope covers source reads, extension launch, and provider
transient failures. Stable fact keys and policy revision in the claim identity
keep duplicate delivery idempotent. Revocation or incompatible-core decisions
are terminal for that policy revision, not retryable provider failures.

Do not solve policy races by reducing workers or serializing all extension
claims. The real fix is policy-revision fencing, idempotent claim keys,
commit-time validation, and bounded dead-letter behavior.

## Observability

Future implementation must expose enough signal for operators to answer which
gate denied work and whether claims are still progressing:

- policy decision counter by state, reason class, trust mode, collector kind,
  and isolation profile;
- claim-capable reconciliation counter and duration;
- claim launch, heartbeat, commit, retry, terminal, and dead-letter counters;
- active policy revision and revocation count in `/admin/status`;
- structured audit log for policy decisions with safe component and instance
  handles;
- no raw scope values, credential handles, provider URLs, source payloads, or
  token-bearing values in metric labels.

No-Observability-Change: this design adds no runtime, worker, queue, graph
query, credential read, metric, span, log, status field, fact kind, Helm value,
or Compose profile. Implementation PRs must add observability evidence before
claim-capable hosted execution is enabled.

## Security Review Questions

Security review must answer these before implementation starts:

1. Which isolation profile is sufficient for community extension code in
   Docker Compose and Kubernetes?
2. Are credential environment variable names safe to show in status, or should
   status expose only credential-source classes?
3. Which revocation reason classes require immediate pod termination versus
   stopping new claims and rejecting commits?
4. Which tenant or workspace scope fields are safe before per-team tokens and
   hosted ACLs land?
5. What evidence is required before a lifecycle channel can be called
   hosted-ready?
6. Does strict provenance require Sigstore/Cosign before any hosted execution,
   or may allowlist plus digest pinning be accepted for first-party test
   deployments only?
7. Which egress destinations are acceptable for the first reference extension?
8. What audit retention is required for denied policy decisions, revocations,
   and credential-reference failures?

## Implementation Sequencing

The follow-up work is already tracked and should not be hidden in PR text:

| Issue | Dependency |
| --- | --- |
| #1819 | Strict provenance verification for plugin trust. |
| #1820 | Activation-to-workflow-coordinator scheduling and revocation behavior. |
| #1825 | API, MCP, and CLI inventory/diagnostics for hosted component state. |
| #1922 | Core extension host adapter and claim/fact validation path. |
| #1923 | Remote Docker Compose proof before hosted rollout claims. |
| #1852 | Per-team hosted tokens or equivalent read-surface isolation. |

Implementation PRs must include TDD coverage for disabled, enable-only,
claim-capable, revoked, incompatible, invalid-config, missing credential
reference, stale policy revision, duplicate coordinator reconciliation,
duplicate delivery, retryable provider failure, terminal provider failure,
in-flight revocation before commit, and restart recovery.

Minimum gates for implementation:

- focused Go tests for component policy, workflow coordinator, extension host,
  and status/diagnostics paths;
- collector authoring gate when a new hosted collector family or extension
  runtime is added;
- performance evidence gate when workers, claims, queues, runtime settings, or
  telemetry paths change;
- Helm lint and render tests when chart values or templates change;
- remote Docker Compose proof before an EKS/GitOps hosted rollout claim;
- strict MkDocs and `git diff --check` for docs and hygiene.
