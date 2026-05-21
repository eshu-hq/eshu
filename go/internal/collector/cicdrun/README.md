# internal/collector/cicdrun

`internal/collector/cicdrun` owns fixture-backed CI/CD provider normalization
for the `ci_cd_run` collector family. It turns offline provider payloads into
reported-confidence fact envelopes.

This package does not call hosted provider APIs, manage credentials, ingest
logs, write graph state, or promote deployment truth.

## Core Responsibilities

- Normalize offline GitHub Actions fixture payloads.
- Emit pipeline definition, run, job, step, artifact, trigger, environment, and
  warning facts.
- Preserve provider-native IDs and run attempts in fact identity.
- Strip query-bearing artifact download URLs before fact emission.
- Emit `ci.warning` facts for partial provider payloads instead of claiming full
  coverage.

## Evidence Boundary

Facts use `source_confidence=reported` because the fixture represents provider
runtime metadata. CI success and environment observations are evidence only.
Reducers decide whether stronger artifact, registry, cloud, source, or
deployment anchors exist.

## Telemetry Boundary

This package emits no metrics, spans, or logs because it is an offline
normalizer. A hosted runtime must add provider API request, rate-limit,
fact-emission, partial-generation, redaction, and status signals before live
collection is enabled.

## Verification

```bash
go test ./internal/collector/cicdrun -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/cicdrun --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- [Collector Readiness](../../../../docs/public/reference/collector-reducer-readiness.md)
- [Collector Authoring](../../../../docs/public/guides/collector-authoring.md)
