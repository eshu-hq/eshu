# Community Extension Authoring

Use this page when a community contributor wants to build an optional Eshu
extension, and when a maintainer needs to review the extension before it is
trusted in a local or hosted runtime.

The short rule is unchanged: extensions may observe source truth and emit
versioned facts. Reducers, projectors, graph writers, query handlers, and answer
packet builders own canonical Eshu truth.

For deciding whether an existing in-tree collector is eligible to move out of
tree, start with the
[Collector Extraction Policy](../reference/collector-extraction-policy.md).

## Choose The Boundary First

Pick one extension boundary before writing code or docs.

| Boundary | Extension may do | Extension must not do | Start with |
| --- | --- | --- | --- |
| Collector component | Observe an external source or repository artifact, normalize source records, and emit durable facts with source confidence. | Write canonical graph rows, apply store DDL, shape final answer prose, or claim truth across sources. | [Collector Authoring](../guides/collector-authoring.md) |
| Parser or language support | Add parser registry metadata, parser implementation, fixtures, and query proof for a language surface. | Document parse-only behavior as supported, or promote unsupported graph behavior. | [Language Support](../contributing-language-support.md) |
| Relationship mapping | Emit explainable relationship evidence and let resolver/reducer stages admit canonical relationships. | Lower confidence thresholds, inflate weak evidence, or write graph edges directly from extraction code. | [Relationship Mapping](../reference/relationship-mapping.md) |
| Semantic enrichment | Preserve optional semantic observations or code hints with provider, policy, redaction, and admission provenance. | Send provider traffic without source policy, store raw prompts or keys, or treat model output as graph truth. | [Semantic Enrichment Posture](../reference/semantic-enrichment-posture.md) |
| Answer enrichment | Build a prompt-facing view over an existing canonical response envelope. | Invent new truth, hide unsupported capability errors, or attach confident prose when evidence is missing. | [Answer Packet Contract](../reference/answer-packets.md) |

If the boundary is unclear, stop and settle ownership before implementation.
Wrong graph or answer truth is a product failure, even when the extension is
optional.

## Quickstart

Start with the scaffold command, then keep the generated contract explicit:

```bash
eshu component init collector \
  --id dev.example.collector.demo \
  --publisher example \
  --fact-kind dev.example.demo_observation
```

For a working package that follows this shape, see the
[Reference Scorecard Extension](reference-scorecard-extension.md).

By default the command writes a new `./dev.example.collector.demo` directory.
Use `--output <dir>` to choose a different new directory. The command refuses
unsafe identifiers and existing output directories so it does not overwrite
local config or secrets.

The scaffold creates:

- `manifest.yaml` for the component package.
- `README.md` explaining source scope, fact kinds, privacy posture, and local
  verification.
- sample configuration with placeholder values only.
- SDK sample code that emits facts through `collector-sdk/v1alpha1`.
- focused tests that fail if manifest fact declarations, source confidence, SDK
  contract, and emitted facts disagree.
- `<your-component>/scripts/verify-local.sh` for local Go tests and component
  inspect/verify — generated inside the new component directory, not a script
  in this repo.

After scaffolding:

1. Decide the source scope and generation identity. A collector scope might be a
   repository, account, region, cluster, registry target, dataset, or provider
   tenant slice. The generation must let consumers distinguish current evidence
   from stale rows.
2. Define every emitted fact before projection:
   - `fact_kind`
   - `schema_version`
   - `collector_kind`
   - `source_confidence`
   - stable fact key
   - redacted payload shape
   - downstream reducer, projector, or query consumer contract
3. Write the manifest. The first component manifest API is
   `eshu.dev/v1alpha1`, `kind: ComponentPackage`, and
   `spec.componentType: collector`. Required fields and local trust behavior are
   defined in [Component Package Manager](../reference/component-package-manager.md).
4. Verify the manifest locally:

```bash
eshu component inspect ./manifest.yaml
eshu component verify ./manifest.yaml \
  --trust-mode allowlist \
  --allow-id dev.example.collector.demo \
  --allow-publisher example
```

5. Validate emitted fixture results against the manifest-derived SDK contract:

```bash
eshu component conform ./manifest.yaml \
  --fixture ./testdata/fixtures/complete-result.json \
  --mode fixture \
  --json
```

Fixture conformance is fail-closed for undeclared fact kinds, unsafe payload
keys, unsupported schema versions, conflicting duplicate stable keys, and
reducer phases that do not have an optional-component consumer yet. A passed fixture
run does not prove Docker Compose, reducer graph truth, or API query truth.

6. If the manifest and fixtures pass, exercise local package-manager state:

```bash
eshu component install ./manifest.yaml \
  --component-home ./.eshu-components \
  --trust-mode allowlist \
  --allow-id dev.example.collector.demo \
  --allow-publisher example

eshu component enable dev.example.collector.demo \
  --component-home ./.eshu-components \
  --instance demo-local \
  --mode scheduled \
  --claims \
  --config ./config.local.yaml

eshu component list --component-home ./.eshu-components
```

For hosted process proofs, the activation config can declare the public source
identity the SDK claim should carry:

```yaml
host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
```

Only those fields leave the local activation config. Commands, credentials,
provider URLs, and private paths must stay in local config or environment
variables. Without a `host` block, component work uses a synthetic component
scope and should be treated as package-manager provenance, not proof of a real
external target.

Local install and enable prove local registry, activation, and local claim state
only. They do not prove hosted readiness, provenance verification, reducer
admission, graph truth, or query truth.

7. Run the smallest verification gate that proves the touched boundary:

| Change | Minimum proof |
| --- | --- |
| Docs or navigation only | Strict MkDocs build and `git diff --check`. |
| Collector family or hosted collector runtime | Collector authoring gate plus focused collector, fact, reducer, and runtime tests for the changed surface. |
| Collector cassette or no-provider replay proof | The five-command conformance flow and the replay gates in [Cassette and Replay Proof](../reference/cassette-replay.md). |
| Parser or language support | Parser tests, integration or query proof, affected language docs, `scripts/verify-parser-relationship-kit.sh`, and docs build. |
| Relationship mapping | Extractor/evidence tests with positive, negative, and ambiguous fixtures, reducer/materialization tests, query or story proof, `scripts/verify-parser-relationship-kit.sh`, and docs build. |
| Runtime performance or hosted activation | Before/after or no-regression evidence plus observable metrics, traces, logs, or status fields. |

Use [Local Testing](../reference/local-testing.md) as the gate map.

## Manifest And Fact Contract

Community components must be explicit enough for automated review.

| Contract | Requirement |
| --- | --- |
| Identity | `metadata.id`, `metadata.publisher`, and `metadata.version` are stable, lowercase, and allowlistable. |
| Compatibility | `spec.compatibleCore` names the supported Eshu core range. Release builds enforce it; local `dev` builds still parse it. |
| Artifacts | Every artifact image is digest-pinned with a full SHA256 digest. Mutable tags are not acceptable. |
| Runtime protocol | `spec.runtime.sdkProtocol` declares the collector SDK protocol, currently `collector-sdk/v1alpha1`, and `spec.runtime.adapter` declares the host adapter. The first hosted worker runs `process`; `oci` is reserved for the digest-pinned adapter path. |
| Fact schemas | `spec.emittedFacts[]` declares fact kinds, optional payload schema refs, schema versions, and source-confidence values. |
| Namespacing | Optional components use collision-resistant fact kinds. Core Eshu fact kinds remain core-owned. |
| Source confidence | New output uses `observed`, `reported`, `inferred`, or `derived`. `unknown` is compatibility debt, not normal component output. |
| Consumer contract | `spec.consumerContracts.reducer.phases` or equivalent structured metadata declares which reducer or query consumer can interpret the fact. |
| Telemetry | `spec.telemetry.metricsPrefix` names component-owned metrics when the component emits metrics. |

If a fact has no consumer, it can be stored as provenance, but it must not appear
as active platform truth. See [Fact Schema Versioning](../reference/fact-schema-versioning.md)
and [Fact Envelope Reference](../reference/fact-envelope-reference.md).
For what the reducer promises about delivery, ordering, generation
supersession, unconsumed fact kinds, and dead-letter visibility, see
[Reducer Guarantees](../reference/reducer-guarantees.md).
Trust policy and failure behavior live in
[Plugin Trust Model](../reference/plugin-trust-model.md).

## Collector SDK Compatibility

The first public collector SDK module lives at `sdk/go/collector` with Go module
path `github.com/eshu-hq/eshu/sdk/go/collector`. It is a public compatibility
boundary and must not import `github.com/eshu-hq/eshu/go/internal/...`.

The initial SDK module semver line is `v0.1.x`. The initial wire protocol is
`collector-sdk/v1alpha1`, and the checked-in JSON Schema artifact is
`sdk/go/collector/schema/collector-sdk-v1alpha1.schema.json`. See
[SDK Compatibility](sdk-compatibility.md) for the full SDK-version /
core-release / wire-protocol / fixture-pack-version compatibility table and
the exact tag versions to pin.

Use the SDK types and validator to emit:

- `Claim`, `Scope`, and `Generation` records from a core-owned work item.
- `Fact` records with declared fact kind, schema version, stable key,
  source confidence, source reference, redacted payload, and optional tombstone.
- `Status` and `Result` records for `complete`, `unchanged`, `partial`,
  `retryable`, and `terminal` outcomes.

`NewValidator(contract).ValidateResult(result)` fails closed before host commit
when a result uses undeclared fact kinds, unsupported schema versions,
`source_confidence=unknown`, mismatched source references, unsafe source URIs,
credential-looking payload keys, unsupported tombstones, or conflicting
duplicates. Exact duplicate facts are reported as idempotent duplicates.

This SDK module does not launch extensions, claim workflow rows, or commit facts
by itself. Those host-side contracts are separate implementation issues.

### Run conformance outside the monorepo

The conformance harness used by `eshu component conform` is also published as a
public library at `github.com/eshu-hq/eshu/sdk/go/collector/conformance`. An
out-of-tree collector repository can import it — alongside the
`sdk/go/collector` types — and run conformance in its own CI without the `eshu`
binary or any `github.com/eshu-hq/eshu/go/internal/...` package:

```go
var manifest conformance.Manifest
_ = yaml.Unmarshal(manifestBytes, &manifest) // your package's own YAML codec

report := conformance.Run(conformance.Request{
    Manifest: manifest,
    Fixtures: []collector.Result{result},
    Mode:     conformance.ModeFixture,
})
if !report.OK() {
    // report.Findings explains each blocker; report marshals to stable JSON.
}
```

`conformance.Run` is a pure function over a decoded manifest and decoded SDK
results. It fails closed on unversioned fact kinds, missing proof metadata
(identity, compatible-core range, digest-pinned artifact), undeclared or unsafe
fixtures, an unsupported mode, or no fixtures, and emits the same
`eshu.extension.conformance.v1` report the in-tree host produces. A worked
example lives in
`examples/collector-extensions/scorecard/conformance_harness_test.go`. The
in-tree `eshu component conform` command remains the convenience wrapper that
loads the manifest and fixture files from disk for you.

### Validate payload shape against a pinned fixture pack

`conformance.Run` also validates fact **payload shape** when you supply
`Request.PayloadSchemas` — a fact kind mapped to its JSON Schema bytes. A fixture
whose payload omits a schema-required field, carries a wrong-typed field, or is
checked against a schema construct the harness cannot interpret fails closed with
a `payload_schema_invalid` finding that names the offending field. A kind with no
supplied schema is not payload-validated, so provenance-only kinds are
unaffected.

For hosted or CLI conformance, declare the same mapping in the component
manifest with `payloadSchemaRef`:

```yaml
emittedFacts:
  - kind: dev.acme.collector.aws_resource
    payloadSchemaRef: aws_resource
    schemaVersions:
      - 1.0.0
    sourceConfidence:
      - reported
```

The manifest kind remains namespaced and component-owned. The schema ref names
the core fixture-pack shape the host should use for payload validation.

The schemas ship as a versioned, importable **fixture pack** inside the contracts
module, `github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack`. Pin the
`sdk/go/factschema` module at a released version and read the schema (and, if you
want, the pack's own valid/invalid example payloads) from it:

```go
schema, _ := fixturepack.SchemaFor("aws_resource")
report := conformance.Run(conformance.Request{
    Manifest: manifest, // declares dev.acme.collector.aws_resource
    Fixtures: []collector.Result{result}, // emits dev.acme.collector.aws_resource
    Mode:     conformance.ModeFixture,
    PayloadSchemas: map[string]json.RawMessage{
        // Map YOUR namespaced kind to the aws_resource schema SHAPE. The bare
        // core kind "aws_resource" is host-owned and reserved; you emit the
        // same shape under your own namespaced kind. This is the extension
        // contract, not a mismatch.
        "dev.acme.collector.aws_resource": schema,
    },
})
```

The fixture-pack **version is the `sdk/go/factschema` module version** — one git
tag, one lockstep release. Pinning that module at a tag pins the schemas and
example payloads that were valid together at that tag, so a payload shape your
collector can no longer satisfy surfaces as a failed conformance run in your own
CI the moment you bump the pin, before the mismatch ever reaches a reducer. To
cut a new pack, cut a `sdk/go/factschema` release. A worked, out-of-tree example
that pins the pack and proves both the accept and the fail-closed path lives in
`examples/collector-extensions/scorecard/fixturepack_pin_test.go`; see
`sdk/go/factschema/fixturepack/README.md` for the accessor surface and the full
versioning procedure.

The core host adapter lives in `go/internal/collector/extensionhost`. It is a
claim-aware intake adapter, not a plugin API: core builds a bounded JSON
claim/config/contract request, launches a host-provided runner such as the local
process harness, validates the returned SDK result, maps accepted facts to
internal envelopes, and then lets `collector.ClaimedService` commit or mutate
the workflow claim under the existing fence. Extensions never receive direct
Postgres, graph, reducer, API, MCP, or workflow-control handles.

The host adapter path proves process and digest-pinned OCI execution contracts
under the extension host. It does not make hosted provenance verification, Helm
defaults, or remote Compose rollout complete; those remain gated by the
publication policy, reference package, and remote proof issues.

## Local Experimentation Versus Hosted Activation

Local experimentation is allowed to be narrow and explicit:

- use `--component-home` or `ESHU_COMPONENT_HOME` so test state stays isolated
- use `allowlist` mode with the exact component ID and publisher under review
- keep credentials in local ignored files or environment variables
- record whether verification covered manifest shape, fact emission, reducer
  admission, graph truth, query truth, or only local package-manager state

Hosted activation is a separate maintainer and operator decision:

- installing a component is not activating it
- enabling a component is not enough to make it claim-capable in the hosted
  workflow coordinator
- strict trust requires configured Sigstore/Cosign signature and SLSA
  provenance verification for each digest-pinned artifact
- hosted collectors need bounded scopes, read-only permissions, secret handles,
  resource limits, `/healthz`, `/readyz`, `/metrics`, `/admin/status`, and
  queue or retry visibility
- Helm or Compose defaults must not enable a new hosted extension without
  runtime proof and an explicit operator opt-in
- the deployed shared bearer token is not a per-team or per-repository
  isolation boundary

Treat hosted activation as production data-plane work. It needs trust, privacy,
performance, and observability evidence in addition to local manifest success.

## Maintainer Review Checklist

Use this checklist when triaging an extension PR.

### Publication Review Responsibilities

- Confirm the package boundary is eligible for community index publication and
  does not rely on index membership as runtime trust.
- Verify the compatibility badge is generated from manifest, signature,
  provenance, conformance, and policy metadata rather than hand-written copy.
- Keep external marketplace publication blocked while the badge still records
  draft status, placeholder artifact digests, pending signature/provenance
  state, local-only conformance proof, or `missing_proof` policy results.
- Review malicious-package behavior: unallowlisted IDs and publishers, mutable
  artifact tags, undeclared egress, credential-looking payload keys, and direct
  store or graph handles fail closed before activation.
- Review stale-signature behavior: digest mismatches, missing SLSA provenance,
  unsupported attestation predicates, wrong certificate identity, and wrong OIDC
  issuer produce blocked diagnostics instead of installable badges.
- Review compromised-publisher behavior: publisher and component revocation
  override allowlists, installed state, enabled instances, index membership,
  and prior successful verification.
- Review schema-collision behavior: core-owned fact kinds, non-namespaced fact
  kinds, incompatible schema-version majors, undeclared emitted facts, and
  duplicate fact-kind ownership by another component fail before publication or
  hosted activation.
- Confirm hosted enablement is reviewed separately from local experimental
  install, including egress, resource limits, credential references, isolation
  profile, tenant scope, and revocation response.
- Record which reviewer accepted each blocking class and which proof artifact
  supports publication, hosted activation, or both.

### Scope And Ownership

- The PR names exactly one primary boundary: collector, parser, relationship,
  semantic enrichment, or answer enrichment.
- Source truth, scope identity, generation identity, emitted facts, confidence,
  failure model, and operations signals are written down.
- Collector code emits facts only. Graph writes, reducer admission, query
  shaping, and answer prose remain in their owning packages.
- Any new parser claim is backed by registry metadata, implementation, tests,
  language docs, and query proof before it is called supported.
- Any relationship change preserves evidence, resolution, graph materialization,
  and query/story agreement with positive, negative, and ambiguous fixtures.
- Parse-only behavior is not described as supported query, graph, story, or
  dead-code behavior.

### Trust And Hosted Safety

- The manifest uses `eshu.dev/v1alpha1`, `ComponentPackage`, and
  `componentType: collector` with a supported `spec.runtime.sdkProtocol` and
  `spec.runtime.adapter`.
- Artifact references are digest-pinned and do not use mutable tags.
- Local verification uses `disabled`, `allowlist`, or `strict` deliberately.
  `strict` requires the expected Sigstore certificate identity and OIDC issuer,
  and fails closed when signatures or supported attestations are absent.
- Revocation behavior is documented for component ID and publisher.
- Hosted activation is opt-in. Process execution, OCI execution, and Cosign
  verification must each be enabled by explicit deployment policy and proof.
- Any hosted collector has read-only credentials, bounded targets, retry and
  dead-letter behavior, resource limits, and operator status surfaces.

### Privacy And Source Safety

- Examples contain no raw provider keys, bearer tokens, cloud access keys,
  tenant IDs, private hostnames, private repository names, private paths, or
  source payloads.
- Credentials are referenced by environment variable, Kubernetes Secret,
  Vault-like handle, workload identity, or local profile name, never by value.
- Payloads redact source values before fact emission. Logs, metrics, and status
  fields use bounded labels and fingerprints instead of high-cardinality or
  sensitive source data.
- Semantic enrichment keeps source policy, redaction, retention, provider
  profile, prompt version, and admission state separate from graph truth.

### Schema And Consumer Compatibility

- Every emitted fact has a stable key, schema version, collector kind, source
  confidence, scope, generation, observed time, and redacted source reference.
- New fact kinds are namespaced unless they are accepted into core Eshu.
- Unsupported major versions fail clearly. New minor fields are not treated as
  authoritative until the runtime declares support.
- Existing stored facts have a migration or reindex plan when semantics change.
- A reducer, projector, query, or explicit provenance-only contract exists
  before any fact is presented as active platform truth.

### Tests And Verification

- Bug fixes or behavior changes have failing regression coverage before the
  implementation.
- Tests cover positive, negative, empty, stale, duplicate, retry, partial
  failure, and ambiguous cases that fit the boundary.
- Collector or hosted runtime changes include idempotency, retry, claim/fencing,
  ordering, dead-letter, and rollback proof where applicable.
- Relationship changes include extractor/evidence tests, reducer/materializer
  proof, and query/story truth proof.
- Parser changes include parser fixtures, integration or query coverage, and
  updated language pages or matrices.
- Parser and relationship changes pass `scripts/verify-parser-relationship-kit.sh`
  so unsupported maturity claims and missing docs updates are caught before
  review.
- Docs, navigation, and README changes pass strict MkDocs and `git diff --check`.

### Performance And Observability

- Runtime-affecting changes include before/after or no-regression evidence for
  the same input shape.
- Metrics, traces, logs, status, or pprof output let an operator identify the
  source scope, generation, failure class, queue state, and slow stage.
- New metrics use bounded labels. Source IDs, file paths, resource names,
  package names, provider URLs, and credentials do not become metric labels.
- Any `No-Observability-Change:` claim names existing signals that diagnose the
  path.

### Documentation

- Public docs explain local use, hosted limits, trust mode, fact schemas,
  telemetry, privacy posture, and verification gates.
- Contract changes link to the relevant reference pages rather than duplicating
  stale copies.
- Docs avoid promising capabilities that are not implemented or not enabled by
  default, including unreviewed OCI execution, Sigstore/Cosign verification
  without strict trust configuration, hosted default enablement, or
  tenant-scoped hosted tokens.

## Troubleshooting

| Symptom | Likely cause | What to check |
| --- | --- | --- |
| `component inspect` rejects the manifest | Wrong `apiVersion`, `kind`, missing metadata, unsupported component type, malformed semantic version, or mutable artifact tag. | Compare the manifest with [Component Package Manager](../reference/component-package-manager.md). |
| `component verify` rejects in allowlist mode | Component ID or publisher does not exactly match the allowlist, or revocation blocks it. | Re-run with the exact `--allow-id` and `--allow-publisher` under review. |
| Strict trust mode fails | The Cosign verifier, expected certificate identity, expected OIDC issuer, digest claims, or supported SLSA provenance are missing or do not match. | Treat this as expected fail-closed behavior, not proof that provenance passed. |
| Facts ingest but graph answers do not change | The fact is provenance-only, unsupported by the reducer/query consumer, stale by generation, or below relationship confidence. | Check the consumer contract, reducer/materializer proof, and query/story proof. |
| A new relationship is missing | Evidence did not resolve, confidence stayed below threshold, aliases were missing, or reducer materialization has not completed. | Follow [Relationship Mapping Observability](../reference/relationship-mapping-observability.md). |
| Semantic observations are absent | No provider profile, no matching source policy, disabled source class, exhausted budget, or redaction/policy denial. | Use [Semantic Enrichment Posture](../reference/semantic-enrichment-posture.md) status checks. |
| Answer enrichment returns unsupported or partial | The underlying envelope is unsupported, stale, truncated, missing evidence, or running under a profile that cannot answer authoritatively. | Read [Reading Eshu Answers](../reference/reading-answers.md). |
| Hosted collector pod is healthy but no central status appears | The runtime is not registered, not claim-capable, not emitting durable status, or only visible through pod-local endpoints. | Check [Collector Runtime Services](../deployment/service-runtimes-collectors.md). |
| Docs build fails after adding the page | Missing MkDocs nav entry, broken relative link, duplicate anchor, or invalid Markdown extension syntax. | Run the strict MkDocs command from [Local Testing](../reference/local-testing.md). |

## Related Docs

- [SDK Compatibility](sdk-compatibility.md)
- [Component Package Manager](../reference/component-package-manager.md)
- [Plugin Trust Model](../reference/plugin-trust-model.md)
- [Fact Schema Versioning](../reference/fact-schema-versioning.md)
- [Fact Envelope Reference](../reference/fact-envelope-reference.md)
- [Reducer Guarantees](../reference/reducer-guarantees.md)
- [Collector Authoring](../guides/collector-authoring.md)
- [Collector Extraction Policy](../reference/collector-extraction-policy.md)
- [Language Support](../contributing-language-support.md)
- [Relationship Mapping](../reference/relationship-mapping.md)
- [Semantic Enrichment Posture](../reference/semantic-enrichment-posture.md)
- [Answer Packet Contract](../reference/answer-packets.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Local Testing](../reference/local-testing.md)
