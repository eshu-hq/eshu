# Collector Conformance Harness

This package is the public, out-of-tree-runnable conformance harness for Eshu
collector component packages. It lets a collector repository prove its package
shape and SDK output without the `eshu` binary, the Eshu monorepo, or any
`github.com/eshu-hq/eshu/go/internal` package.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/collector/conformance`
- Module: `github.com/eshu-hq/eshu/sdk/go/collector` (no third-party dependencies)
- Report schema: `eshu.extension.conformance.v1`

## What it checks

`Run` evaluates one already-decoded manifest and the package's decoded
collector-sdk/v1alpha1 result fixtures:

- **Config / manifest proof metadata** — identity, compatible-core range
  (comparator syntax is validated, not just presence), digest-pinned artifact,
  SDK protocol, and host adapter.
- **Fact schema** — every emitted fact kind is namespaced and declares at least
  one semantic schema version; fixtures only emit declared kinds/versions. When
  `Request.ReservedFactKinds` is supplied (the in-tree host passes the core
  fact-kind registry), a manifest that claims a host-owned kind fails closed.
- **Payload shape** — when `Request.PayloadSchemas` maps a fact kind to its JSON
  Schema, every fixture fact of that kind is validated against it and fails
  closed (`payload_schema_invalid`) on a missing required field, a wrong-typed
  field, or a schema construct the validator cannot interpret (it never passes a
  payload it could not actually validate). A kind with no supplied schema is not
  payload-validated, so provenance-only kinds are unaffected. The caller supplies
  the schema bytes — the in-tree host reads `sdk/go/factschema/schema/*.json`, an
  out-of-tree collector reads them from the pinned
  `sdk/go/factschema/fixturepack` — so this package stays dependency-free and
  performs no I/O. `CompileSchema` lets a caller pre-check that a schema is
  within the supported subset, and `ValidatePayloadSchemas` runs only this
  payload-shape check for runtime hosts that have already applied the SDK result
  contract.
  For host-driven conformance, a manifest can declare `payloadSchemaRef` on an
  `emittedFacts[]` entry so the in-tree host maps the namespaced component kind
  to that fixture-pack schema shape before calling this harness.
- **Redaction** — fixtures are rejected for credential-bearing payload keys or
  source URIs (delegated to the SDK validator).
- **Claim lifecycle** — fixtures carry a complete claim, generation, and source
  reference that agree with each other.
- **Status reporting and retry behavior** — status classes, partial/retryable
  states, and `retry_after_seconds` are validated per the SDK contract.
- **Reducer consumer contract** — optional component facts may only declare the
  `source_evidence_only:no_graph_truth` reducer phase today.

It fails closed (`status: failed`) on unversioned fact kinds, missing proof
metadata, undeclared or unsafe fixtures, an unsupported mode, or when no fixture
is supplied.

## Usage

```go
raw, _ := os.ReadFile("manifest.yaml")
var manifest conformance.Manifest
_ = yaml.Unmarshal(raw, &manifest) // your package already depends on a YAML codec

result, _ := mycollector.Collect(claim, report, opts) // a collector.Result

report := conformance.Run(conformance.Request{
    Manifest: manifest,
    Fixtures: []collector.Result{result},
    Mode:     conformance.ModeFixture,
})
if !report.OK() {
    // report.Findings explains every blocker; report marshals to stable JSON.
}
```

The harness leaves manifest YAML decoding to the caller so this module stays
dependency-free; `Manifest` carries both `yaml` and `json` struct tags. A
working out-of-tree example lives in
`examples/collector-extensions/scorecard/conformance_harness_test.go`.

## Scope

Fixture conformance proves package shape and SDK result validity. It does not
prove hosted activation, graph truth, API/MCP readback, or production safety —
those require the Compose and reducer/query proofs described in the
[Collector Extraction Policy](../../../../docs/public/reference/collector-extraction-policy.md).
