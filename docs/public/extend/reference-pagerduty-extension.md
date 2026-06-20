# Reference PagerDuty Extension

The reference PagerDuty component package lives at
`examples/collector-extensions/pagerduty`. It is a fixture-only process
component that proves an out-of-tree collector can emit PagerDuty-shaped source
evidence through the public collector SDK without importing Eshu internals or
receiving direct database, graph, API, or MCP handles.

## What It Proves

- The component manifest can declare `collector-sdk/v1alpha1`, a process
  adapter, a trustable package identity, and namespaced PagerDuty fact kinds.
- The SDK result mirrors the in-tree PagerDuty collector contract for the six
  committed source families: incident, lifecycle event, change, observed
  service, observed integration, and coverage warning.
- Component facts remain `source_evidence_only:no_graph_truth`; reducers and
  query surfaces continue to own canonical graph truth.
- The hosted component worker can read a trusted activation from the shared
  component registry, claim PagerDuty work, and commit only bounded source
  evidence through the normal collector commit boundary.

The package does not call PagerDuty, store credentials, publish an OCI image,
or change PagerDuty graph/query semantics.

## Local Verification

From the package directory:

```bash
go test ./...
scripts/test-local-component-lifecycle.sh
```

From the repository root, the in-tree parity test compares the reference
component output with the core PagerDuty fact contract:

```bash
cd go && go test ./internal/collector/pagerduty -run ReferenceComponent -count=1
```

## Remote Compose Proof Harness

The PagerDuty component-extension proof uses normalized artifacts captured from
a running Compose stack after the `dev.eshu.examples.pagerduty` component has
been installed, enabled, trusted, and processed by
`collector-component-extension`.

The verifier is self-tested without a running stack:

```bash
scripts/test-verify-remote-e2e-pagerduty-component-extension.sh
```

To inspect or verify a captured run:

```bash
scripts/verify-remote-e2e-pagerduty-component-extension.sh --list
scripts/verify-remote-e2e-pagerduty-component-extension.sh --artifacts <run-dir>
```

To capture proof artifacts from a live stack where the component host is
already running:

```bash
docker build -t eshu:local -f Dockerfile .
docker compose -p pd-ce-proof \
  -f docker-compose.yaml \
  -f docs/public/run-locally/docker-compose.component-extension-pagerduty.yaml \
  --profile component-extension-collector up -d --build
scripts/run-remote-e2e-pagerduty-component-extension.sh --artifacts <run-dir>
```

The overlay builds the `examples/collector-extensions/pagerduty/Dockerfile`
proof image, installs and enables the fixture component into the shared
component home, exposes that registry to the API and MCP runtimes, starts the
workflow coordinator in active claims mode, and runs the component-extension
collector under an allowlist plus restricted egress policy.

The capture driver records only bounded operational evidence:

- `inventory.json` - component id plus installed, enabled, trusted, and manifest
  digest readback.
- `api-inventory.json` and `mcp-inventory.json` - hosted API and MCP inventory
  readback proving the same component is installed, enabled, and claim-capable.
- `workflow-items.json` - PagerDuty component workflow item ids and terminal
  states.
- `facts.json` - committed `dev.eshu.examples.pagerduty.*` fact-family counts.
- `parity.json` - current run, source-run, generation, and work-item identity
  plus expected and persisted fact signatures for the in-tree and extension
  paths.
- `provenance.json` - commit, component digest, backend, queue terminal state,
  and a port-only metrics handle.
- `disable.json`, `post-disable-inventory.json`, `uninstall.json`, and
  `post-uninstall-inventory.json` - local lifecycle rollback proof after the
  claim/fact evidence has been captured.

The verifier requires the component to be installed, enabled, and trusted; no
workflow item may be retrying, failed, or dead-lettered; all six fact families
must have non-zero committed counts for the captured generation; parity must be
recorded as passed; the expected and extension fact signatures must match; and
the provenance fields must be present. API and MCP inventory must read the
shared component home, and rollback proof must show disable removing active
claim state before uninstall removes the package readback. The signatures cover
fact kind, schema version, stable key, source confidence, source ref, and
payload for the same claim identity used by the running worker. A redaction
canary fails closed if artifacts contain host-local paths, private-key markers,
bearer tokens, or raw IP addresses.

## Helm Enablement

The Helm chart keeps the component extension collector default-off. Operators
enable it with `componentExtensionCollector.enabled=true`, an allowlist trust
mode, an explicit hosted-extension egress policy JSON, and a shared component
registry volume mounted into both the workflow coordinator and worker. See
[Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md#collector-values)
for the value contract and render-time guardrails.

Strict trust mode is not charted until provenance verifier values become
first-class chart inputs. Rollback is disabling `componentExtensionCollector`
and removing the corresponding registry mount and hosted-extension egress
policy from the workflow coordinator values.

## Related Docs

- [Community Extension Authoring](community-extension-authoring.md)
- [Reference Scorecard Extension](reference-scorecard-extension.md)
- [Component Package Manager](../reference/component-package-manager.md)
- [PagerDuty Evidence Contract](../reference/pagerduty-evidence.md)
