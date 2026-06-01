# Image Analyzer

## Purpose

`imageanalyzer` adapts scanner-worker image unpacking and component extraction
to Eshu source facts. It consumes configured local image evidence, either an
already-extracted rootfs or bounded local OCI layer tar streams, and emits
installed OS package source facts when Alpine apk or Debian dpkg package
database evidence exists. Every supported image target also emits
`scanner_worker.analysis` coverage evidence so operators can distinguish a
completed scan from package facts alone.

## Ownership boundary

This package owns image/rootfs package metadata extraction for
`image_unpacking` scanner-worker claims. It does not pull images from
registries, manage credentials, fetch advisories, match vulnerabilities, write
graph state, or publish reducer findings.

## Exported surface

See `doc.go` for the godoc contract. The exported surface is `Analyzer`,
`NewAnalyzer`, `AnalyzerConfig`, `TargetConfig`, `Snapshot`, and
`EvidenceSource`.

## Dependencies

- `internal/collector/ospackagevulnerability` provides Alpine apk and Debian
  dpkg parser contracts plus fact-envelope construction.
- `internal/collector/ospackagevulnerability/osruntime` provides the
  already-extracted rootfs reader used when `rootfs_path` is configured.
- `internal/collector/scannerworker` provides analyzer input, output, resource
  limits, and failure classes.
- `internal/facts` supplies durable source fact envelopes.
- `internal/scope` supplies the scanner-worker collector identity for warning
  facts.

## Telemetry

This package emits no metrics or spans directly. `scannerworker.Service`
records claim, retry, dead-letter, queue-wait, scan-duration, target-count,
result-count, fact-kind, CPU, and memory metrics around this analyzer. The
analyzer returns peak layer/rootfs metadata bytes in `ResourceUsage` so the
hosted runtime can record resource telemetry.

## Gotchas / invariants

- Local layer paths must be ordered from base to top layer.
- Layer unpacking extracts only `/etc/os-release`, `/etc/apk/repositories`,
  `/lib/apk/db/installed`, and `/var/lib/dpkg/status`; it does not materialize a
  whole rootfs.
- OCI whiteout markers are honored for the package metadata paths before facts
  are emitted.
- Unsupported image shapes emit `scanner_worker.warning` facts with
  `analysis_status=not_scanned`, `coverage_status=unsupported`, and an
  `extraction_reason`, not clean results.
- Image identity must include an image digest before package facts are emitted;
  tag-only or otherwise incomplete image targets are unsupported coverage
  evidence.
- Missing local layer files are retryable target-unavailable failures; missing
  package databases in readable image evidence are unsupported coverage gaps.
- Rootfs and layer paths must stay out of retry, dead-letter, metric, and log
  payloads.

## Evidence Contract

Emitted `scanner_worker.analysis` facts mark supported image targets as
`analysis_status=completed` and `coverage_status=scanned` without asserting
impact. Emitted `vulnerability.os_package` facts preserve the image reference,
image digest, source URI/record, evidence source (`rootfs` or `layer`),
package manager, package name, installed version, distro, and distro release
when the package database supplies them. Unsupported image shapes, malformed
layer evidence, missing package databases, and missing image digests emit
`scanner_worker.warning` facts with bounded status and `extraction_reason`
fields instead of silent clean output.

No-Regression Evidence: `go test ./internal/collector/scannerworker/imageanalyzer ./internal/collector/ospackagevulnerability/osruntime ./internal/collector/scannerworker ./cmd/scanner-worker -count=1` covers layer/rootfs extraction, unsupported evidence, resource limits, output validation, and runtime wiring.

Observability Evidence: the analyzer uses existing scanner-worker claim,
retry, dead-letter, facts-emitted, queue-wait, scan-duration, target-count,
result-count, CPU, memory, and private pprof signals; no new metric dimensions
or labels are introduced.

## Related docs

- `../README.md`
- `../../ospackagevulnerability/README.md`
- `docs/public/reference/security-intelligence.md`
- `docs/public/reference/collector-reducer-readiness.md`
