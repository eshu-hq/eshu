# ADR: Component Package Manager And Optional Collector Activation

**Date:** 2026-05-10
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `../reference/component-package-manager.md`
- `../reference/fact-envelope-reference.md`
- `../reference/fact-schema-versioning.md`
- `../reference/plugin-trust-model.md`

---

## Context

Eshu started with Git as the default source because every engineering team has
code. The platform is now adding optional source families: Terraform state,
AWS, Kubernetes, OCI registries, SBOMs, vulnerability feeds, documentation
systems, and other runtime or security evidence.

Shipping every collector in every deployment is the wrong default. A company
that does not use AWS should not carry AWS runtime code, IAM policy, Helm
values, or operational surface. A local Eshu install should stay useful with
Git-first defaults. Security-heavy teams should be able to add only the
collector families they need.

The existing platform already has good seams:

- facts are versioned and reducer-owned
- plugin trust policy is documented
- collector instances have enabled and claim-capable state
- the workflow coordinator can reconcile desired collector instances

What is missing is the package boundary between "this component exists" and
"this component is allowed to run."

## Decision

Add an Eshu component package manager.

The component manager owns package inspection, local verification, install
state, and activation records. It does not make collectors authoritative by
itself. Reducers and query surfaces still decide which facts can become active
truth.

The first implementation is local and air-gapped first:

- load `ComponentPackage` manifests from disk
- validate identity, publisher, version, artifacts, fact families, and consumer
  contracts
- verify against a local trust policy
- install the manifest into a local component registry
- enable or disable local activation records

OCI pull and Sigstore/Cosign verification are future verifier backends. Strict
mode fails closed until that backend exists. The system must not claim
provenance guarantees it cannot prove.

## Component States

Component state is split deliberately:

- **Available** means a package can be inspected.
- **Verified** means the manifest passed local compatibility and trust checks.
- **Installed** means the package is recorded in the component registry.
- **Enabled** means an activation record exists for an instance.
- **Claim-capable** means the activation is allowed to claim workflow work.

Installed is not enabled. Enabled is not claim-capable.

## CLI Contract

The public command group is `eshu component`.

Initial commands:

- `eshu component inspect <manifest>`
- `eshu component verify <manifest>`
- `eshu component install <manifest>`
- `eshu component list`
- `eshu component enable <component-id> --instance <id>`
- `eshu component disable <component-id> --instance <id>`
- `eshu component uninstall <component-id> --version <version>`

The legacy `eshu add-package` command remains a compatibility stub. Component
packages are runtime extensions, not source-language package indexing.

## Manifest Shape

The first manifest version is:

```yaml
apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.aws
  name: AWS cloud scanner
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.1.0"
  componentType: collector
  collectorKinds:
    - aws
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/components/aws-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.aws.cloud_resource
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - cloud_resource_uid:canonical_nodes_committed
  telemetry:
    metricsPrefix: eshu_dp_aws_
```

Artifacts must be digest-pinned with SHA256. Mutable tags, missing digests, and
short digest strings are not enough for install records or audit.

Release builds enforce `compatibleCore` against the running Eshu core version.
Local source builds that report `dev` parse the range but do not enforce a
release comparison.

## Trust Policy

Trust modes are:

- `disabled`: reject optional components
- `allowlist`: require an allowed component ID and publisher
- `strict`: require allowlist and provenance verification

The first implementation supports disabled and allowlist locally. Strict mode
returns a clear failure until real provenance verification is wired in.

Revocation is explicit. Operators may revoke component IDs or publishers
without turning off all component support.

## Runtime Boundary

The component manager does not run arbitrary plugin code in-process. A future
runtime launcher may start a component as a process, container, job, or external
endpoint, but that launcher must preserve Eshu's admin, telemetry, queue,
claim, and fact contracts.

Go shared-library plugins are not part of this design because they couple
toolchains and can crash the host process. Separate processes or containers are
the safer long-term shape.

## Consequences

Positive:

- local Eshu stays small
- optional collectors have a consistent install and activation model
- trust failures are visible before runtime work starts
- future OCI distribution has a clear manifest and registry target

Tradeoffs:

- the first slice does not pull OCI artifacts
- strict provenance verification is intentionally unavailable until a real
  verifier is implemented
- installed component state starts as a local registry, not a central policy
  service

## Acceptance

This ADR is accepted when:

- `eshu component` can inspect, verify, install, list, enable, disable, and
  uninstall local component manifests
- invalid, disabled, revoked, and strict-unverified packages fail closed
- docs explain installed versus enabled versus claim-capable
- tests cover manifest, trust policy, local registry, and CLI behavior
