# Component Package Manager

Eshu components are optional runtime packages. They let operators add source
families such as cloud collectors, Kubernetes collectors, SBOM collectors, or
vulnerability intelligence without making every Eshu deployment carry every
integration.

Git remains built in. Optional collectors are installed only when an operator
chooses them.

## State Model

Component state has separate steps:

| State | Meaning |
| --- | --- |
| Available | A local manifest can be inspected from disk. |
| Verified | The manifest passed local compatibility and trust policy checks. |
| Installed | The manifest is recorded in the local component registry. |
| Enabled | The component has an activation record for a named instance. |
| Claim-capable | The activation is allowed to claim workflow work. |

Installed is not enabled. Enabled is not claim-capable.

## Commands

The CLI commands are local-state operations:

```bash
eshu component inspect ./aws-component.yaml
eshu component verify ./aws-component.yaml \
  --trust-mode allowlist \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq
eshu component install ./aws-component.yaml \
  --component-home ~/.eshu/components \
  --trust-mode allowlist \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq
eshu component enable dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --instance prod-aws \
  --mode scheduled \
  --claims \
  --config ./aws-collector.yaml
eshu component list --component-home ~/.eshu/components
eshu component disable dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --instance prod-aws

eshu component uninstall dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --version 0.1.0
```

Uninstall fails while a component has active instances.

## Component Home

The CLI resolves component home in this order:

1. `--component-home`
2. `ESHU_COMPONENT_HOME`
3. `ESHU_HOME/components`
4. `~/.eshu/components`

The first implementation stores:

- `registry.json`
- copied manifests under `packages/<component-id>/<version>/manifest.yaml`

Registry writes use a temporary file and rename in the same directory.

## Trust Modes

| Mode | Behavior |
| --- | --- |
| `disabled` | Reject all optional components. |
| `allowlist` | Require allowed component ID and publisher. |
| `strict` | Fail closed until provenance verification is wired in. |

Revocation can block a component ID or publisher. Revocation wins over
allowlists.

## Community Extension Index

The community extension index is a reviewed discovery document for component
packages. Index membership is advisory: it helps operators find a candidate
manifest, artifact digest, publisher, review record, compatible core range,
emitted facts, and revocation state, but it never bypasses local trust policy.

Operators must still run local verification before install, choose disabled,
allowlist, or strict mode, honor revocation, and explicitly enable any runtime
instance. Hosted deployments also need hosted policy approval before an enabled
component can become claim-capable.

The first verifier is offline and deterministic. It rejects malformed index
metadata, duplicate component IDs, duplicate fact-kind claims, mutable artifact
tags, malformed digests, unsupported lifecycle channels, missing review links,
and revoked entries marked installable. It does not pull OCI registries, treat
GitHub topics as authoritative trust, or perform provenance verification.

## Manifest

The first manifest version is `eshu.dev/v1alpha1`.

Required manifest fields:

| Field | Contract |
| --- | --- |
| `apiVersion` | Must be `eshu.dev/v1alpha1`. |
| `kind` | Must be `ComponentPackage`. |
| `metadata.id` / `metadata.publisher` | Lowercase identifier. Revocation and allowlists match these values exactly. |
| `metadata.version` | Semantic version. |
| `spec.compatibleCore` | Core version range. Release builds enforce it; local `dev` builds parse it but skip release comparison. |
| `spec.componentType` | Currently only `collector`. |
| `spec.collectorKinds` | One or more collector-family identifiers. |
| `spec.artifacts[].image` | Digest-pinned image with a full SHA256 digest. |
| `spec.emittedFacts[]` | Fact kind, schema versions, and source-confidence values emitted by the component. |
| `spec.consumerContracts.reducer.phases` | Reducer phase contracts the emitted fact kinds need. |
| `spec.telemetry.metricsPrefix` | Component-owned metric prefix, when the component emits metrics. |

Artifacts must be digest-pinned with a SHA256 digest. Mutable tags and short
or malformed digests are rejected.

`compatibleCore` is checked during verification. Release builds compare the
manifest range against the running Eshu core version. Local source builds that
report `dev` still parse the range but do not enforce a release comparison.

Each `emittedFacts` entry must declare `sourceConfidence`. Allowed values are
`observed`, `reported`, `inferred`, and `derived`. `unknown` is reserved for
old stored rows and system fallback data; component manifests cannot declare it
as normal emitted output.

## Current Limits

This first slice does not pull from OCI registries and does not perform
Sigstore/Cosign verification. Strict mode fails closed instead of pretending to
verify provenance.

The activation record is local package-manager state. Workflow coordinator
scheduling still belongs to the existing collector instance control plane.
