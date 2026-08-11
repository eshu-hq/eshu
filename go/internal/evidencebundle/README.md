# evidencebundle

## Purpose

`evidencebundle` owns the deterministic `evidence_bundle.v1` schema, its
builders, renderer, and validator. A bundle packages selected answer packets,
investigation packets, capability catalog handles, surface inventory handles,
freshness/readiness state, missing evidence, and reproduce calls into one
share-safe snapshot.

There are two builders. `BuildDemoBundle` produces the deterministic fixture
used as a schema example. `BuildLiveBundle` shapes a caller-supplied
`LiveSnapshot` of a running stack's status into the same schema, adding
`contents.pipeline_state` and `contents.semantic_provider_state`. Both are
pointer fields with `omitempty`, so a demo bundle renders neither and its
`bundle_id` is unaffected by the live addition.

## Ownership boundary

The package does not read Git, Postgres, graph backends, local files, HTTP APIs,
MCP sessions, providers, or GitHub. Callers provide already-resolved evidence —
including the `LiveSnapshot` handed to `BuildLiveBundle` — or use the
deterministic demo builder. API, MCP, and CLI surfaces stay in their
own packages and call this package as a pure composer/validator.

## Privacy contract

Bundles use handles and route/tool/command names, not private endpoints,
credentials, prompts, provider responses, raw source blobs, or local absolute
paths. Validation rejects those canaries before a bundle is accepted.

## Verification

Focused package gate:

```bash
cd go && go test ./internal/evidencebundle -count=1
```

CLI integration is covered by:

```bash
cd go && go test ./cmd/eshu -run 'TestEvidenceBundle|TestRootCommandIncludesEvidenceBundle' -count=1
```
