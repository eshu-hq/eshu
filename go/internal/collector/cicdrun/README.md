# CI/CD Run Collector Contracts

## Purpose

`internal/collector/cicdrun` owns fixture-backed CI/CD provider normalization
for the `ci_cd_run` collector family. It turns offline provider payloads into
reported-confidence fact envelopes that reducers can consume.

This package intentionally does not implement hosted API polling, credentials,
log ingestion, graph writes, or deployment truth promotion.

## Exported Surface

- `CollectorKind` — durable collector family name: `ci_cd_run`.
- `ProviderGitHubActions` — provider value used for GitHub Actions facts.
- `FixtureContext` — scope, generation, collector instance, fencing token,
  observed time, and source URI copied into emitted envelopes.
- `GitHubActionsFixtureEnvelopes` — parses one offline GitHub Actions fixture
  and returns CI/CD fact envelopes.

## Invariants

- Provider-native IDs and run attempts are part of fact identity, so retries do
  not overwrite prior attempts.
- Facts use `source_confidence=reported` because the fixture represents provider
  runtime metadata.
- Artifact download URLs are stripped when they carry query strings.
- Missing or partial provider payloads emit `ci.warning` facts instead of
  silently claiming complete coverage.
- CI success and environment observations remain evidence only. Reducers decide
  whether stronger artifact or deployment anchors exist.

## Telemetry

This package emits no metrics, spans, or logs. Hosted runtime telemetry belongs
in a later collector runtime slice. The fixture-backed proof is bounded by the
number of runs, jobs, steps, artifacts, triggers, and warnings in one fixture.

No-Regression Evidence: fixture normalization is covered by
`go test ./internal/collector/cicdrun -run TestGitHubActionsFixture -count=1`,
which exercises one successful run, retry-attempt identity, missing artifact
digest warnings, and partial job metadata warnings without graph writes or
queue work.

No-Observability-Change: this package is a deterministic offline normalizer and
does not mount a runtime. The later hosted runtime slice must add provider API
request, rate-limit, fact-emission, partial-generation, redaction, and status
signals before live collection is enabled.
