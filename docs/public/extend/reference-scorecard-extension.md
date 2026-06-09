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
