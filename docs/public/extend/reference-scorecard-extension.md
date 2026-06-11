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

It does not prove hosted scheduling, OCI pull, Sigstore or Cosign provenance,
API/MCP extension inventory, reducer admission, graph truth, or answer changes.
Those remain core-owned follow-up lanes.

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
