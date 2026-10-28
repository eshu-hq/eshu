# Image Analyzer

## Purpose

`imageanalyzer` adapts scanner-worker image unpacking and component extraction
to Eshu source facts. It consumes configured local image evidence, either an
already-extracted rootfs or bounded local OCI layer tar streams, and emits
installed OS package source facts when Alpine apk, Debian dpkg, or RPM package
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

- `internal/collector/ospackagevulnerability` provides Alpine apk, Debian dpkg, and RPM
  parser contracts plus fact-envelope construction.
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
  `/lib/apk/db/installed`, `/var/lib/dpkg/status`, and recognized RPM databases; it does not materialize a
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

No-Regression Evidence: #4790 keeps the scanner-worker fact cardinality and
status semantics unchanged while moving `scanner_worker.analysis` and
`scanner_worker.warning` payload construction from local maps to
`factschema.EncodeScannerWorker*`. Baseline was the pre-change raw-map builder:
one analysis fact per supported image target, one warning fact per unsupported
target, unchanged stable-key inputs, unchanged `fact_count` calculation, and
no queue/work-item claim changes. After measurement ran the same image-analyzer
input shape through the typed seam: `go test ./internal/collector/scannerworker/imageanalyzer -count=1`,
`go test ./... -count=1` from `sdk/go/factschema`, and `make pre-pr` all
passed locally on July 7, 2026. Backend/version is not applicable because this
package writes no graph, SQL, or backend query state; terminal source-fact
counts remain the envelope counts above and are still reported through the
scanner-worker host.

No-Observability-Change: #4790 adds no new scanner-worker metrics, spans, log
fields, pprof labels, queue status fields, or telemetry dimensions. The
analyzer still relies on the hosted scanner-worker claim, retry, dead-letter,
facts-emitted, queue-wait, scan-duration, target-count, result-count, CPU,
memory, and private pprof signals; the typed encoder only changes the local
construction seam for the same payload keys.

## Satellite-contract source identity (Refs #9385)

The analysis and unsupported-warning facts now carry the same sanitized
source identity as their sibling package facts (`SourceURI` through the
shared `ospackagevulnerability.SanitizeURL`, record falling back from the
snapshot record to the image digest), because the collector SDK satellite
contract requires `source_ref.uri` and `record_id` on every fact. Stable
keys, payloads, limits, and the per-package emission path are untouched.

No-Regression Evidence: throwaway `BenchmarkAnalyzeFixedRootFS` (fixed
60-package Alpine rootfs, 61 facts per claim, linux/arm64, go1.26.6).
Baseline `a5252ea478`: 477,000-502,000 ns/op, ~538,450 B/op, 6,465
allocs/op. After: 481,000-488,000 ns/op, ~538,700 B/op, 6,468 allocs/op.
Timing overlaps run-to-run noise; the delta is +3 allocs/op (+0.05%) and
+~250 B/op (+0.05%) from the two sanitizer calls on the analysis/warning
facts. Safe because the per-package hot path is untouched and fact ceilings
are unchanged (`TestMaxFactsContractUnchanged` still pins 50,000).

No-Observability-Change: the new fields add no metric, label, span, or log
key. Envelope counts and the scanner-worker claim/retry/dead-letter signals
are unchanged; `TestAnalyzerPagedProjectionParity` proves paged reassembly
keeps identical kinds, stable keys, generations, and scopes.

## Related docs

- `../README.md`
- `../../ospackagevulnerability/README.md`
- `docs/public/reference/security-intelligence.md`
- `docs/public/reference/collector-reducer-readiness.md`
