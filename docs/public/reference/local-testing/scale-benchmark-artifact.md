# Scale Benchmark Artifact

Issue #3171 records the public-safe benchmark artifact contract for
large-corpus ingestion, reducer, graph-write, API, and MCP proof. The contract
lives in `specs/scale-benchmark-artifact.v1.yaml` and is checked by
`scripts/verify-scale-benchmark-artifact.sh`.

Use this gate after the representative corpus contract has been accepted and
before any optimization claims readiness. The verifier does not run a benchmark
itself. It validates that the benchmark result artifact records the required
metadata, metrics, thresholds, backend matrix, before/after evidence, and
privacy posture.

## Required Artifact Shape

Every accepted artifact must record:

- run id, run kind, issue number, proof gate, backend kind and version, and the
  exact commit SHA measured;
- the `scale-lab-corpus/v1` corpus slot, mode, repository count, and
  public-safe privacy status;
- NornicDB proof plus the supported compatibility backend row, or an explicit
  reason when the compatibility backend is skipped or unsupported for that run;
- fact rows/sec, queue claim p95, reducer drain wall time, graph write p95,
  API p95, MCP p95, retry count, dead-letter count, and memory high-water mark;
- numeric thresholds and pass/fail threshold results for every metric;
- sanitized artifact handles for aggregate results, thresholds, pprof, and log
  evidence;
- pprof, log, and resource snapshot status;
- before/after baseline commit and artifact handles for any optimization claim.

Top-level `status=pass` requires every metric threshold to pass and requires
`retry_count` and `dead_letter_count` to be zero.

## Privacy Boundary

Public artifacts must stay aggregate-only. Do not include private repository
names, provider locators, alert locators, account ids, tokens, email addresses,
hostnames, IP addresses, local machine paths, raw logs, raw request or response
bodies, package names from private systems, or transcripts.

Raw pprof captures, logs, resource snapshots, and operator-local source
manifests stay outside the repository. The public artifact should reference
only sanitized aggregate handles.

## Verification

Validate the repository contract and verifier tests:

```bash
bash scripts/test-verify-scale-benchmark-artifact.sh
bash scripts/verify-scale-benchmark-artifact.sh
```

Validate an operator-produced public artifact:

```bash
bash scripts/verify-scale-benchmark-artifact.sh \
  --artifact scale-benchmark-artifact.json
```

This gate complements `scripts/verify-performance-evidence.sh`: hot-path code
changes still need tracked benchmark or no-regression evidence in the repo,
while this artifact gate defines the shape of the benchmark proof itself.
