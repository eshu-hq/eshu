# Reference Scorecard Extension

The reference Scorecard collector package lives at
`examples/collector-extensions/scorecard`. It is the copyable example for
community authors who need a working component manifest, public SDK usage,
fixtures, and local package-manager proof without private credentials.

## What It Proves

- A community package can import `github.com/eshu-hq/eshu/sdk/go/collector`
  without importing Eshu core internals.
- A component manifest can declare `collector-sdk/v1alpha1`, an `oci` adapter,
  digest-pinned artifact guidance, namespaced fact kinds, reported source
  confidence, source-evidence-only reducer posture, and a metrics prefix.
- Local tests can prove complete, unchanged, partial, retryable, duplicate, and
  manifest-agreement behavior before hosted activation exists.
- `eshu component inspect`, `verify`, `install`, `enable`, and `list` can show
  the package in local CLI inventory with isolated component state.

It does not prove hosted scheduling, Sigstore or Cosign provenance, API/MCP
extension inventory, reducer admission, graph truth, or answer changes. Those
remain core-owned follow-up lanes. OCI image pull, digest resolution, and
container isolation are proven separately by the OCI adapter proof below.

## Fact Families

| Fact kind | Source confidence | Role |
| --- | --- | --- |
| `dev.eshu.examples.scorecard.snapshot` | `reported` | One report-level Scorecard observation. |
| `dev.eshu.examples.scorecard.check` | `reported` | One normalized Scorecard check result. |
| `dev.eshu.examples.scorecard.warning` | `reported` | Low-score, empty, or duplicate-source warnings. |

The manifest declares `source_evidence_only:no_graph_truth` as the reducer
contract. These facts are provenance until a separate reducer or query contract
promotes any part of them.

## Local Verification

From the package directory:

```bash
go test ./...
go run ./cmd/scorecard-collector --input ./testdata/complete.json
scripts/test-local-component-lifecycle.sh
```

The lifecycle script uses a temporary component home and allowlist policy, then
checks local CLI readback. It does not mutate the operator's normal
`~/.eshu/components` state.

## Remote Compose Proof Harness

The remote Compose proof (issues #2126/#1923) runs the component through the
`collector-component-extension` worker against a stack and verifies the result.
The verifier checks recorded harness artifacts against the proof invariants and
is self-tested independently of any running stack:

```bash
# Self-test the verifier (no stack required):
scripts/test-verify-remote-e2e-component-extension.sh

# Verify a recorded proof run (artifacts captured from a stack):
scripts/verify-remote-e2e-component-extension.sh --artifacts <run-dir>
scripts/verify-remote-e2e-component-extension.sh --list   # show the checks
```

To run the proof against a live stack, layer the component-extension overlay on
the base Compose file. It builds a proof image (the eshu runtime base plus the
reference `scorecard-collector` binary and an idempotent install/enable init),
shares one component home across the coordinator and worker, and puts the
coordinator in active/claims-enabled mode under an allowlist trust policy and an
explicit hosted-extension egress allow rule:

```bash
docker build -t eshu:local -f Dockerfile .
docker compose \
  -f docker-compose.yaml \
  -f docs/public/run-locally/docker-compose.component-extension.yaml \
  --profile component-extension-collector up -d --build

# Capture runtime truth into normalized artifacts and verify them:
scripts/run-remote-e2e-component-extension.sh --artifacts <run-dir>
```

`run-remote-e2e-component-extension.sh` reads the component CLI trust readback,
the `workflow_work_items` terminal states, and the committed
`dev.eshu.examples.scorecard.*` fact counts from the running stack, emits the
three artifacts below (counts and states only, never payloads), and runs the
verifier against them. A passing run reports `installed`/`enabled`/`trusted`
true, every scorecard work item `completed`, and non-zero `snapshot`, `check`,
and `warning` fact families.

The artifacts directory holds four bounded captures from the run:

- `inventory.json` — the `GET /api/v0/component-extensions` readback; the
  verifier requires `installed`, `enabled`, and `trusted` true for
  `dev.eshu.examples.scorecard`.
- `workflow-items.json` — the component workflow items; the verifier requires a
  terminal `completed`/`succeeded` state and fails closed on any
  `retrying`/`failed`/`dead_letter` item.
- `facts.json` — committed fact counts; the verifier requires at least one
  `dev.eshu.examples.scorecard.*` family with a non-zero count.
- `provenance.json` — the immutable run identity: `eshu_commit`,
  `component_digest`, `core_version`, `sdk_version`, `backend`,
  `queue_terminal_state`, and a port-only `metrics_handle`. The verifier
  requires every field present and non-empty, the commit resolved (not
  `unknown`), and the digest a `sha256:` value, so a run records what built it
  and where it ran. Only version strings, a digest, and a port-only telemetry
  handle are recorded — never a host path, hostname, IP, or credential.

A redaction canary fails the proof if any artifact contains a host-local path,
private-key marker, bearer token, or raw IP address. Scorecard facts remain
source evidence only — the proof asserts no graph nodes or edges are written for
them until a reducer/query contract promotes them. The worker honors the
manifest adapter: `process` for local runs and `oci` for a digest-pinned
artifact (see the component extension collector).

## OCI Adapter Proof

The process-adapter harness above proves the SDK claim/commit boundary, but it
cannot prove image pull, digest resolution, or container isolation. The OCI
adapter proof closes that gap with a standalone, minimal reference image
(`examples/collector-extensions/scorecard/Dockerfile.oci`: the pure-Go
`scorecard-collector` binary plus its baked fixture on a distroless non-root
base) and a live verifier:

```bash
scripts/verify-oci-scorecard-adapter.sh           # build, push, resolve digest, run isolated
scripts/verify-oci-scorecard-adapter.sh --list    # show the checks
```

The verifier builds the image, pushes it to a registry (a throwaway local
`registry:2` by default), resolves the immutable `repo@sha256:<digest>`
reference, then launches that digest-pinned artifact through the exact contract
`extensionhost.OCIRunner` uses:

```
docker run --rm -i --network none --read-only \
  --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
  <repo>@sha256:<digest>
```

It feeds one SDK host request on stdin (its `config.source.input` points at the
in-image fixture, never a host path) and asserts the three
`dev.eshu.examples.scorecard.*` families come back on stdout, with the same
redaction canary applied. No network, a read-only rootfs, dropped capabilities,
and a non-root user mean the artifact receives no Eshu Postgres, graph, reducer,
API, MCP, or workflow handles — only the bounded request. Publishing the image
to a shared registry and pinning the resulting digest in the manifest artifact
is the remaining registry-publish step.

### End-to-End In-Compose Run

The verifier above proves the OCI run contract in isolation. The in-Compose
harness (issue #2156) proves the remaining single-container wiring: the running
`component-extension-collector` worker claims a Scorecard work item, launches the
digest-pinned artifact with `docker run` through the host container runtime, and
commits `dev.eshu.examples.scorecard.*` facts through `collector.ClaimedService`
— in one Compose run.

```bash
# Self-test the OCI verifier (no stack required):
scripts/test-verify-remote-e2e-component-extension-oci.sh

# Full live run: registry + build/push + digest + Compose up + capture + verify:
scripts/run-remote-e2e-component-extension-oci.sh --artifacts <run-dir>
```

`run-remote-e2e-component-extension-oci.sh` stands up a dedicated local
`registry:2`, builds and pushes `Dockerfile.oci`, resolves the immutable
`repo@sha256:<digest>` reference, and layers
`docs/public/run-locally/docker-compose.component-extension-oci.yaml` on the base
Compose file. That overlay builds an OCI worker image
(`examples/collector-extensions/scorecard/oci.worker.Dockerfile`: the eshu base
plus the `docker` CLI), mounts the host runtime socket into the collector
service, and pins the manifest artifact (`manifest.oci.yaml`, `adapter: oci`) to
the resolved digest in the shared component home. The worker reads the
digest-pinned image only from the verified manifest artifact, never from
operator config, and launches it under the same `--network none --read-only
--user 65532:65532 --cap-drop ALL --security-opt no-new-privileges` isolation as
the standalone verifier.

The driver captures the shared `inventory.json`, `workflow-items.json`,
`facts.json`, and `provenance.json` artifacts, then adds `provenance-oci.json`
recording `adapter: oci` and the digest-pinned `oci_image`. The OCI verifier
(`scripts/verify-remote-e2e-component-extension-oci.sh`) runs every shared check
plus: `adapter` is `oci`, `oci_image` is digest-pinned (`repo@sha256:<64 hex>`),
the run provenance fields are present, and the redaction canary holds over the
OCI provenance. Scorecard facts remain source evidence only — the proof asserts
no graph nodes or edges are written for them.

The worker runs as root in the proof solely to read the mounted runtime socket;
the launched artifact stays confined by the adapter isolation flags. The base
`eshu:local` image must be built from the same checkout, since the worker image
layers on it — the driver rebuilds it by default.

## Hosted Limits

The reference manifest uses a placeholder digest so local validation can prove
shape. A published extension must replace the image with the digest of the
actual artifact. Hosted activation also needs policy approval, resource limits,
status surfaces, and runtime proof before it can become claim-capable outside
local component-manager state.

## Related Docs

- [Community Extension Authoring](community-extension-authoring.md)
- [Component Package Manager](../reference/component-package-manager.md)
- [Collector Authoring](../guides/collector-authoring.md)
- [Fact Envelope Reference](../reference/fact-envelope-reference.md)
- [Fact Schema Versioning](../reference/fact-schema-versioning.md)
