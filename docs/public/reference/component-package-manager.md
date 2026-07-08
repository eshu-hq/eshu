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
eshu component init collector \
  --id dev.example.collector.demo \
  --publisher example \
  --fact-kind dev.example.demo_observation
eshu component inspect ./aws-component.yaml
eshu component verify ./aws-component.yaml \
  --trust-mode allowlist \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq
eshu component verify ./aws-component.yaml \
  --trust-mode strict \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq \
  --provenance-certificate-identity https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0 \
  --provenance-oidc-issuer https://token.actions.githubusercontent.com
eshu component install ./aws-component.yaml \
  --component-home ~/.eshu/components \
  --trust-mode allowlist \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq
eshu component install ./aws-component.yaml \
  --component-home ~/.eshu/components \
  --trust-mode allowlist \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq \
  --dry-run \
  --json
eshu component conform ./aws-component.yaml \
  --fixture ./testdata/fixtures/complete-result.json \
  --mode fixture \
  --json
eshu component index verify ./community-extension-index.yaml \
  --json
eshu component enable dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --instance prod-aws \
  --mode scheduled \
  --claims \
  --config ./aws-collector.yaml
eshu component list --component-home ~/.eshu/components
eshu component list --component-home ~/.eshu/components \
  --trust-mode allowlist \
  --allow-id dev.eshu.collector.aws \
  --allow-publisher eshu-hq \
  --revoke-id dev.eshu.collector.old \
  --json
eshu component disable dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --instance prod-aws

eshu component uninstall dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --version 0.1.0

eshu component inventory \
  --service-url https://eshu.example \
  --limit 100 \
  --json
eshu component diagnostics dev.eshu.collector.aws \
  --service-url https://eshu.example \
  --json
eshu component extraction-readiness pagerduty \
  --verbose \
  --json
```

`component init collector` writes a new scaffold directory. It defaults to
`./<component-id>` and also accepts `--output <dir>`. It refuses existing output
directories, unsafe identifiers, and non-namespaced fact kinds. The generated
package contains a manifest, sample SDK collector code, tests, placeholder-only
config, a README, and `scripts/verify-local.sh`.

`component conform` runs a read-only extension conformance check against one
manifest and one or more collector SDK result fixtures. Fixture mode loads the
manifest through the component contract, derives the host SDK validator contract
from `spec.emittedFacts`, validates every fixture, repeats host validation so
the supplied result must stay stable under re-validation, and reports whether
findings block publication or hosted activation. `--mode compose` is accepted
as a reserved mode label, but this slice still requires explicit fixtures; it
is not remote Docker Compose proof.

Uninstall fails while a component has active instances.

Every component subcommand accepts `--json`. JSON output uses
`schema_version: eshu.component.cli.v1`, a command name, a status, and the
component, activation, verification, conformance, list, or error block that
applies to the command. The text output remains the default operator summary.

`install --dry-run` verifies the manifest and trust policy but does not create
`registry.json` or copy the manifest. `enable --dry-run` validates the selected
component and activation instance but does not write an activation.

Example install dry run:

```json
{
  "schema_version": "eshu.component.cli.v1",
  "command": "install",
  "status": "would_install",
  "dry_run": true,
  "component": {
    "id": "dev.eshu.collector.aws",
    "name": "AWS cloud scanner",
    "publisher": "eshu-hq",
    "version": "0.1.0"
  },
  "verification": {
    "allowed": true,
    "mode": "allowlist",
    "component": "dev.eshu.collector.aws",
    "publisher": "eshu-hq",
    "version": "0.1.0"
  }
}
```

Example list output:

```json
{
  "schema_version": "eshu.component.cli.v1",
  "command": "list",
  "status": "listed",
  "components": [
    {
      "id": "dev.eshu.collector.aws",
      "name": "AWS cloud scanner",
      "publisher": "eshu-hq",
      "version": "0.1.0",
      "manifest_digest": "sha256:...",
      "verified": true,
      "trust_mode": "allowlist",
      "installed_at": "2026-06-09T00:00:00Z",
      "states": ["installed", "enabled", "claim_capable"],
      "activations": [
        {
          "instance_id": "prod-aws",
          "mode": "scheduled",
          "claims_enabled": true,
          "config_path": "./aws-collector.yaml",
          "enabled_at": "2026-06-09T00:00:00Z"
        }
      ]
    }
  ]
}
```

Example conformance output:

```json
{
  "schema_version": "eshu.component.cli.v1",
  "command": "conform",
  "status": "passed",
  "conformance": {
    "schema_version": "eshu.extension.conformance.v1",
    "mode": "fixture",
    "status": "passed",
    "component_id": "dev.eshu.collector.aws",
    "component_version": "0.1.0",
    "summary": {
      "fixture_count": 1,
      "fact_count": 4,
      "duplicate_count": 0,
      "redaction_count": 0,
      "tombstone_count": 0,
      "status_count": 1,
      "idempotent_reemission_checked": true
    }
  }
}
```

Conformance findings are fail-closed. Invalid manifests, missing fixtures,
unreadable fixture JSON, undeclared fact kinds, unsafe payload keys, unsupported
schema versions, unsupported source confidence values, unsupported tombstones,
conflicting duplicate stable keys, and reducer phases without a current
optional-component consumer block both publication and hosted activation.

`component index verify <index>` runs the offline community extension index gate
that is suitable for CI and maintainer-local review. It accepts YAML or JSON,
then rejects entries with mutable artifact refs, malformed digests, unsupported
lifecycle channels, missing review links, revoked installable entries,
core-owned or non-namespaced fact-kind claims, invalid fact schema versions,
unsupported source confidence values, missing reducer consumer contracts,
missing provenance signature refs, or missing/failed conformance proof refs.
The command does not call registries, APIs, graph backends, or Postgres.

The stable readback states are:

| State | Meaning |
| --- | --- |
| `installed` | The package is present in `registry.json`. |
| `enabled` | One or more activation records exist. |
| `claim_capable` | At least one activation can claim workflow work. |
| `revoked` | Policy re-verification marked the component ID or publisher revoked. |
| `incompatible` | Policy re-verification found an incompatible core range. |
| `failed` | Manifest readback or policy re-verification failed. |

`component list --json` accepts the same trust and revocation flags as
`verify`. When those flags are omitted, list reports stored lifecycle state and
local manifest readback only. When trust or revocation flags are supplied, list
also re-verifies installed manifests and can add `revoked`, `incompatible`, or
`failed` states without mutating the registry.

`component inventory` and `component diagnostics <component-id>` are API-backed
readers. They do not inspect the caller's local component home; they read the
configured API or MCP runtime's `ESHU_COMPONENT_HOME` through
`GET /api/v0/component-extensions`. Inventory is bounded by `--limit` (default
100, max 500) and mirrors the API response fields `count`, `total_count`,
`limit`, and `truncated`. When the runtime has no component home, the response
is a canonical unavailable envelope with
`component_registry_unavailable`. Hosted responses include component ID,
version, publisher, manifest digest, installed/enabled/claim-capable states,
revocation or policy failure reasons, stable activation `config_handle` values,
`trust_decision`, `policy_gate`, `last_conformance_proof`, `scheduler_state`,
and `read_model_availability`. They do not include local manifest paths,
activation config paths, provider credentials, or private host paths.

`component extraction-readiness [collector-family]` prints the advisory collector
extraction readiness checklist. For each collector family the extraction policy
tracks it reports a classification (`keep_in_tree`, `extraction_candidate`,
`blocked`, or `external_ready`), and `--verbose` adds the per-criterion
checklist; `--json` emits the machine-readable form. Unlike `inventory` and
`diagnostics`, it reads no API or registry state — the data is static policy
classification compiled into the CLI, so it runs offline and never moves code.
See [Collector Extraction Policy](collector-extraction-policy.md) for the
classification vocabulary and the seven criteria.

## Hosted Coordinator Activation

The workflow coordinator can consume the same local registry when
`ESHU_COMPONENT_HOME` is explicitly set on the coordinator process. Hosted
activation is fail-closed: `ESHU_COMPONENT_TRUST_MODE=allowlist` requires
matching `ESHU_COMPONENT_ALLOW_IDS` and `ESHU_COMPONENT_ALLOW_PUBLISHERS`.
`ESHU_COMPONENT_TRUST_MODE=strict` requires the same allowlist plus the
Sigstore/Cosign provenance settings described below. Revoked IDs, revoked
publishers, incompatible core ranges, unsupported runtime protocols,
unsupported adapters, missing signatures, and unsupported provenance shapes
stop new workflow claims.

The coordinator materializes trusted claim-capable activations as normal
`collector_instances` rows, then plans one activation-scoped workflow item for
the manifest-declared collector kind. The durable collector instance stores a
component ID, version, publisher, manifest digest, runtime protocol, adapter,
and stable `config_handle`. It does not store activation config paths,
provider targets, or credential values. Use `eshu component list --json` with
the same trust and revoke flags to inspect local policy failure reasons.

An activation config may include a safe `host` block:

```yaml
host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
```

When present, the workflow coordinator copies only those three public fields
into collector instance configuration and planned work rows. `sourceSystem` and
`scope.id` become the SDK claim's source identity; `scope.kind` tells the
process-backed worker which SDK scope kind to send. The raw config path,
process command, credentials, and provider-specific config stay local. When the
`host` block is absent, the coordinator falls back to a synthetic
`component:<stable-id>` scope for activation-scoped provenance.

Component extension workflow rows are source evidence only until a core reducer
contract consumes the emitted facts. The coordinator does not create graph
nodes or edges for extension facts.

Hosted operators can check the same redacted posture through API or MCP:

```bash
curl -H 'Accept: application/eshu.envelope+json' \
  "$ESHU_SERVICE_URL/api/v0/component-extensions?limit=100"
```

One component row reports whether trust is allowed or blocked, which policy gate
won, whether the last conformance proof is still missing, whether scheduler
work can be claimed, and whether a component read model is available. Example
blocked states include `disabled_by_policy`, `incompatible`,
`missing_conformance_proof`, and `runtime_failure`.

JSON errors use stable codes:

| Code | Meaning |
| --- | --- |
| `invalid_manifest` | The manifest cannot be read, decoded, or validated. |
| `incompatible_core` | The manifest's `compatibleCore` range excludes the running Eshu core. |
| `revoked_package` | The component ID or publisher is revoked. |
| `untrusted_publisher` | The local trust policy does not allow the component or publisher. |
| `provenance_required` | Strict mode is missing a verifier, certificate identity, or OIDC issuer. |
| `provenance_invalid` | Cosign signature, digest-claim, or identity verification failed. |
| `unsupported_provenance` | Signed attestation material is absent or uses an unsupported predicate shape. |
| `active_uninstall` | Uninstall was requested for a package version with active instances. |
| `duplicate_activation` | The requested instance is already enabled. |
| `fact_kind_collision` | The manifest claims a fact kind already owned by another installed component. |
| `conformance_failed` | A local component conformance run emitted publication or hosted-activation blockers. |
| `corrupted_registry_state` | `registry.json` cannot be decoded or read consistently. |
| `active_replacement` | Replacement content was supplied for an active installed version. |
| `not_installed` | The requested component, version, or activation is absent. |
| `invalid_input` | A flag or identifier is invalid. |
| `unverified_package` | Install was attempted without a matching successful verification. |
| `registry_write_failed` | A local registry or package-content write failed. |

Error messages avoid stored manifest paths. CLI activation output can show the
operator-selected activation config path because that is local package-manager
state; hosted workflow rows and host-metadata read errors do not echo it.

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
The v1 CLI keeps that atomic-write behavior for install, enable, disable, and
uninstall. Dry-run commands do not write either the registry file or package
content.

## Trust Modes

| Mode | Behavior |
| --- | --- |
| `disabled` | Reject all optional components. |
| `allowlist` | Require allowed component ID and publisher. |
| `strict` | Require allowed component ID and publisher, then verify every digest-pinned OCI artifact with Cosign signature claims and a supported SLSA provenance attestation. |

Revocation can block a component ID or publisher. Revocation wins over
allowlists.

Strict mode treats the manifest publisher as an Eshu policy identifier, not as
the Sigstore certificate identity. Operators must pass the expected certificate
identity and OIDC issuer explicitly with
`--provenance-certificate-identity` and `--provenance-oidc-issuer`. The default
attestation predicate type is `slsaprovenance1`; other predicate shapes are
rejected until the verifier contract is expanded.

The Cosign check uses `cosign verify` with claim checking enabled and requires
signature annotations that bind the manifest's component ID, publisher, version,
compatible core range, SDK protocol, runtime adapter, collector kinds, and
emitted fact kinds, schema versions, source-confidence values, reducer phases,
and telemetry prefix. It then runs `cosign verify-attestation --type
slsaprovenance1` for the same artifact. Eshu does not accept registry token or
password flags on the component command; registry authentication must come from
Cosign's normal environment, keychain, or workload identity handling so CLI
errors never echo credential values.

## Community Extension Index

The community extension index is a reviewed discovery document for component
packages. Index membership is advisory: it helps operators find a candidate
manifest, artifact digest, publisher, review record, compatible core range,
emitted facts, and revocation state, but it never bypasses local trust policy.

Operators must still run local verification before install, choose disabled,
allowlist, or strict mode, honor revocation, and explicitly enable any runtime
instance. Hosted deployments also need hosted policy approval before an enabled
component can become claim-capable.

Do not treat index membership as trust in API, MCP, or CLI diagnostics. The
inventory read surface reports local registry and policy state only: installed,
enabled, and claim-capable remain separate states, and community-index
membership never changes the trust verdict.

The first verifier is offline and deterministic. It rejects malformed index
metadata, duplicate component IDs, duplicate fact-kind claims, mutable artifact
tags, malformed digests, unsupported lifecycle channels, core-owned or
non-namespaced fact-kind claims, invalid schema versions, unsupported source
confidence values, missing reducer consumer contracts, missing signature
references, missing or failed conformance proof references, missing review
links, and revoked entries marked installable. It does not pull OCI registries,
treat GitHub topics as authoritative trust, or perform provenance verification.

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
| `spec.runtime.sdkProtocol` | Collector SDK wire protocol. The first supported value is `collector-sdk/v1alpha1`. |
| `spec.runtime.adapter` | Host adapter shape. The first supported values are `oci` and `process`. |
| `spec.artifacts[].image` | Digest-pinned image with a full SHA256 digest. |
| `spec.emittedFacts[]` | Fact kind, optional payload schema shape, schema versions, and source-confidence values emitted by the component. |
| `spec.consumerContracts.reducer.phases` | Reducer phase contracts the emitted fact kinds need. |
| `spec.telemetry.metricsPrefix` | Component-owned metric prefix, when the component emits metrics. |

Artifacts must be digest-pinned with a SHA256 digest. Mutable tags and short
or malformed digests are rejected.

Runtime protocol fields are checked during manifest validation. Unknown SDK
protocols or adapters are rejected before install or activation. Declaring a
supported runtime protocol does not make an installed package claim-capable:
operators must still enable an instance, hosted policy must approve it, and the
workflow coordinator or extension host must implement the matching adapter. The
component-extension worker supports `process` activations and digest-pinned
`oci` activations through the extension host adapter. Hosted `oci` use still
requires an approved policy, a runnable digest artifact, and runtime proof for
the target deployment.

`compatibleCore` is checked during verification. Release builds compare the
manifest range against the running Eshu core version. Local source builds that
report `dev` still parse the range but do not enforce a release comparison.

Each `emittedFacts` entry must declare `sourceConfidence`. Allowed values are
`observed`, `reported`, `inferred`, and `derived`. `unknown` is reserved for
old stored rows and system fallback data; component manifests cannot declare it
as normal emitted output.

Each `emittedFacts.kind` must be namespaced with a collision-resistant prefix.
Core-owned fact kinds from `go/internal/facts` are reserved and cannot be
claimed by optional components. During install and activation planning, the
local registry compares the manifest against installed component manifests. A
different component ID cannot claim a fact kind that is already installed. The
same component ID can install another version with the same fact kind only when
the declared schema-version major set is compatible; otherwise the registry
returns `fact_kind_collision` with the candidate owner, existing owner, fact
kind, and the operator action needed to proceed. Uninstalling an inactive
component version releases its local fact-kind ownership claim.

An `emittedFacts[].payloadSchemaRef` value is optional. When present, it must
name a fact kind shipped by the factschema fixture pack, such as
`aws_resource`. The component still emits its own namespaced kind, but the host
uses the referenced core payload shape to validate SDK results before
publication or hosted activation. Leave it unset only for provenance-only
component facts that have no core payload shape yet.

## Current Limits

This slice does not pull runnable OCI images into Eshu or execute component
code during verify/install. Strict mode can contact registries through the
operator-selected Cosign verifier to validate signatures and attestations for
digest-pinned artifacts.

Fixture conformance is local validation. It does not run Docker Compose, start
the workflow coordinator, claim work, project graph truth, or prove API/query
truth. Hosted rollout still needs runtime proof after the component package,
SDK adapter, reducer/query consumers, and deployment policy are ready.
