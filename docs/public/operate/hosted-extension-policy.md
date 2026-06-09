# Hosted Extension Operator Policy

Use this page when deciding whether an optional community extension may run in
a hosted Eshu deployment.

Current status: this is operator policy guidance for issue #1826. Hosted
community extension claim execution is not enabled by the shipped chart or
Compose stack yet. Treat Docker Compose and Helm snippets below as the target
policy shape for review, not as currently consumed values, until the workflow
coordinator bridge, extension host, diagnostics, and remote Compose proof land.

## Three Separate Decisions

Do not collapse package state into runtime permission.

| Decision | What it allows | What it does not allow |
| --- | --- | --- |
| Install | The manifest is verified and recorded in component state. | No runtime activation and no source collection. |
| Enable | A named instance has bounded config and credential references. | No workflow claims unless hosted policy also approves it. |
| Claim-capable | The workflow coordinator may create work for the enabled instance. | No graph writes by the extension; reducers still own graph truth. |

Installed is not enabled. Enabled is not claim-capable. Claim-capable is always
operator opt-in and must be revocable.

## Policy Rules

Evaluate policy in this order:

1. Revocation wins over allowlists, index membership, installation, and prior
   enablement.
2. `disabled` rejects all optional components.
3. `allowlist` requires the exact component ID and publisher.
4. `strict` also requires Cosign signature verification, configured Sigstore
   certificate identity and OIDC issuer, digest-claim checking, and a supported
   SLSA provenance attestation for every manifest artifact.
5. Compatible core range, SDK protocol, declared fact kinds, and reducer
   consumer contracts must match the running Eshu core.
6. Enabled instances need bounded source scope, credential references, resource
   limits, network isolation, and a service account or workload identity.
7. Claim-capable instances need active workflow coordination and a hosted policy
   revision that approves the instance.

Use bounded reason classes in tickets and runbooks:
`revoked_policy`, `disabled_policy`, `untrusted_publisher`,
`provenance_required`, `provenance_invalid`, `unsupported_provenance`,
`incompatible_core`, `invalid_config`, `credential_reference_missing`,
`isolation_profile_denied`, `tenant_scope_denied`, and
`claim_capability_denied`.

## Revocation Behavior

Revocation must stop new work without requiring operators to delete package
history.

- Revoke by component ID, publisher, artifact digest, version range, or policy
  revision.
- Stop new coordinator claims for the revoked identity.
- Mark pending work ineligible with a bounded reason.
- Re-check policy before launching an extension and before committing facts.
- Reject fact commits when the policy became revoked before commit.
- Surface component ID, instance ID, reason class, policy revision, and
  effective time in diagnostics.

For compromise-class revocations, scale down or terminate the extension
workload and let existing claim leases expire or be reaped. Revocation cannot
undo a source read that already happened, so commit-time policy checks are the
line that prevents new persisted facts after the stop signal.

## Credential References Only

Policy and examples may contain handles only, never secret values.

| Reference kind | Example handle | Use |
| --- | --- | --- |
| Environment variable | `SCORECARD_API_TOKEN` | Local shell, private Compose env file, or Secret-backed pod env. |
| Kubernetes Secret ref | `scorecard-token/token` | Helm or GitOps Secret wiring. |
| External secret ref | `vault://platform/eshu/scorecard` | Vault or External Secrets style integration. |
| Workload identity | `scorecard-reader` | Cloud provider APIs that support identity-based auth. |
| Local dev profile | `scorecard-local` | Local experiments only. |

Do not put provider keys, bearer tokens, private keys, signed URLs, credential
values, private provider responses, or source payloads in facts, logs, metrics,
docs, issue bodies, component manifests, or public values files. Metric labels
should use credential-source classes, not raw handle names.

## Isolation Checklist

A hosted claim-capable extension needs:

- separate process or container boundary for out-of-tree code;
- no direct Postgres, graph, reducer, API, MCP, or workflow store handles;
- non-root pod user, dropped capabilities, runtime-default seccomp, and
  read-only root filesystem where supported;
- CPU, memory, timeout, input-size, and fact-count limits;
- deny-by-default or allowlisted egress;
- a scoped service account or workload identity;
- `/healthz`, `/readyz`, `/metrics`, and `/admin/status`;
- safe status fields for policy revision, component ID, instance ID, collector
  kind, scope class, and failure reason.

Local experiments can be less isolated, but they are not hosted proof. Remote
Docker Compose proof is required before EKS or GitOps rollout claims.

## Local Example

Local component commands prove local package-manager state only.

```bash
eshu component verify ./manifest.yaml \
  --trust-mode allowlist \
  --allow-id dev.example.collector.scorecard \
  --allow-publisher example

eshu component install ./manifest.yaml \
  --component-home ./.eshu-components \
  --trust-mode allowlist \
  --allow-id dev.example.collector.scorecard \
  --allow-publisher example

eshu component enable dev.example.collector.scorecard \
  --component-home ./.eshu-components \
  --instance scorecard-local \
  --mode scheduled \
  --claims \
  --config ./scorecard.local.yaml
```

Keep config files out of public repositories when they name private source
scopes. Store credentials as references:

```yaml
instance_id: scorecard-local
collector_kind: scorecard
source:
  kind: repository
  selector: local
credential_refs:
  scorecard_api:
    kind: environment_variable
    handle: SCORECARD_API_TOKEN
limits:
  max_input_bytes: 1048576
  max_facts: 500
```

## Docker Compose Policy Shape

The default Compose stack does not consume hosted community extension policy
yet. This shape is the intended private operator input once #1820 and #1922
wire the runtime path:

```bash
export ESHU_HOSTED_EXTENSION_POLICY_FILE=./private/hosted-extension-policy.yaml
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

Example policy file:

```yaml
apiVersion: eshu.dev/v1alpha1
kind: HostedExtensionPolicy
metadata:
  policyId: local-compose-extensions
  revision: "2026-06-09.1"
spec:
  trustMode: allowlist
  allow:
    components:
      - id: dev.example.collector.scorecard
        publisher: example
        versions: ">=0.1.0 <0.2.0"
  instances:
    - instanceId: scorecard-compose
      componentId: dev.example.collector.scorecard
      enabled: true
      claimCapable: false
      collectorKind: scorecard
      scopes:
        - kind: repository
          selector: local
      credentials:
        - name: scorecard_api
          kind: environment_variable
          handle: SCORECARD_API_TOKEN
      isolationProfile: compose-restricted
```

Keep the env file private:

```dotenv
# ./private/scorecard.env
SCORECARD_API_TOKEN=
```

Populate the private value through the operator's secret manager before running
Compose. Do not commit the env file, private policy file, provider target URLs,
or raw source scopes that identify a customer environment.

## Kubernetes And Helm Policy Shape

The Helm chart does not consume this block yet. The target shape is a private
values overlay paired with Secret or workload-identity wiring:

```yaml
hostedExtensions:
  enabled: true
  policy:
    trustMode: allowlist
    allow:
      components:
        - id: dev.example.collector.scorecard
          publisher: example
          versions: ">=0.1.0 <0.2.0"
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
            kind: kubernetes_secret_ref
            secretName: scorecard-extension
            key: api-token
        isolationProfile: hosted-extension-restricted
        tenantScope:
          mode: single_tenant
          workspace: team-a
  isolationProfiles:
    hosted-extension-restricted:
      resources:
        requests:
          cpu: 100m
          memory: 128Mi
        limits:
          cpu: 500m
          memory: 512Mi
      network:
        egress:
          - host: api.scorecard.example.com
            port: 443
      securityContext:
        runAsNonRoot: true
        readOnlyRootFilesystem: true
```

Secret values stay in Kubernetes Secret management, not chart docs or shell
command arguments:

```bash
kubectl create secret generic scorecard-extension \
  --from-env-file=./private/scorecard.env
```

When this path is implemented, render and lint before applying:

```bash
helm template eshu ./deploy/helm/eshu -f values.hosted-extensions.yaml
helm lint ./deploy/helm/eshu -f values.hosted-extensions.yaml
```

## Security Review Posture

Hosted community extension execution must wait for security review and
implementation proof. The gate is not complete until reviewers sign off on:

- whether allowlist plus digest pinning is acceptable for any hosted test
  deployment when strict provenance is not configured for the candidate
  artifact;
- which egress and service-account isolation profile is acceptable;
- whether credential handle names may appear in status or only credential-source
  classes may appear;
- which revocation classes require immediate pod termination;
- what audit retention is required for denied policy decisions and revocations.

Implementation follow-ups are already tracked:

| Issue | Purpose |
| --- | --- |
| #1819 | Strict plugin provenance verification with Cosign identity checks. |
| #1820 | Wire enabled components into hosted workflow coordinator scheduling. |
| #1825 | Expose component inventory and extension diagnostics through API, MCP, and CLI. |
| #1922 | Build the core collector extension host adapter. |
| #1923 | Prove the out-of-tree collector SDK path in remote Compose. |

## Operator Checklist

Before marking an extension hosted-ready:

- verify manifest, digest, publisher, compatible core, and emitted facts;
- confirm install, enable, and claim-capable are separate in status;
- set `claimCapable` to false until isolation, credentials, and revocation are
  reviewed;
- use credential references only;
- define resource limits and egress allowlists;
- document tenant or workspace scope without promising API read isolation;
- prove remote Docker Compose before Kubernetes rollout;
- require strict MkDocs and `git diff --check` for docs changes, plus runtime
  gates for any implementation.
