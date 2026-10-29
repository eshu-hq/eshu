# #9298 Cloud Inventory Allowlist Performance Evidence

Performance Evidence: This note records one bounded, quiet-host validation of
`cloudInventoryRecordFromRow` for AWS inventory facts. It measures the
in-process fact-to-inventory loader, once per source fact row. It does not
measure SQL, graph reads, queue work, network calls, HTTP, or MCP latency.

## Inputs and controls

- Exact source base: `0f8e42c82fd9490c73c26e03964cac1291c304f4`.
- Exact source head: `6c33e395daa9184cd8f2128f107a0b0c77a9754f`.
- Diagnostic prefilter source: `ef315ea2faa96e57c2d6d0c967121a25af563a69`.
- The same benchmark test file was compiled at base and head; its SHA-256 was
  `76ae63b99c16a19d03d09932916378f851d65ab006e3b4e3821c42657e9a42d8`.
  The base worktree changed only this test file for the comparison.
- Runtime: Go 1.26.10, linux/arm64, 16 logical CPUs; no database is used by
  these in-process benchmarks. Queue counts and storage state do not apply.
- The representative ECS input has eight raw attributes. Only
  `task_definition_arn` and filtered `containers` survive the AWS allowlist.
  The adversarial input carries all 11 safe scalars and `containers`, so 12
  keys survive; no single scanner emits that mixed-service shape.
- The host had load1 1.00–1.08 across 16 one-second samples during the paired
  gate, below the predeclared bound of 8. The process canary logged no
  concurrent Go build, test, golden gate, lint, or benchmark. The Docker
  container set was unchanged at start and end. Each base control was below
  the predeclared 8,500 ns/op bound derived outside two prior quiet-host
  control sets (7,953–7,999 ns/op). Those bound-setting controls used earlier
  base `793633b2fe`, whose loader code matches the exact base; they are not
  used as the before/after samples below. The gate exited 0.
- Full local logs: `/tmp/eshu-9298-validation-results.log`,
  `/tmp/eshu-9298-validation-load.log`, and
  `/tmp/eshu-9298-validation-external.log`. These are operator-local logs,
  not committed artifacts. The sample values and controls are preserved below.

## Paired results

Each value is one benchmark process with a one-second target duration.
Same-round ratios compare adjacent samples; SD is sample standard deviation.

| Round | Exact base ECS ns/op | Exact head ECS ns/op | Head/base | Base B/allocs | Head B/allocs |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 7,971 | 8,095 | 1.01556 | 4,192 / 95 | 4,192 / 95 |
| 2 | 7,946 | 8,086 | 1.01762 | 4,192 / 95 | 4,192 / 95 |
| 3 | 7,937 | 8,072 | 1.01701 | 4,192 / 95 | 4,192 / 95 |

The ECS means are 7,951 ns/op at base and 8,084 ns/op at head. The mean
same-round ratio is 1.01673 (SD 0.00106), a measured 1.67% per-row increase
for this input. This is below the 10% healthy-regression investigation
threshold, but it is not a zero-cost result or broad endpoint proof.

The 12-field input cannot be compared to the base as equal-output work: base
does not expose the five new fields. The historical prefilter implementation
and head expose the same 12-field output, so the comparison isolates the
staged-map change. The historical source is named above and its test binary
used the same benchmark file hash.

| Round | Prefilter all-12 ns/op | Head all-12 ns/op | Head/prefilter | Prefilter B/allocs | Head B/allocs |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 8,471 | 8,503 | 1.00378 | 4,824 / 105 | 4,824 / 105 |
| 2 | 8,427 | 8,447 | 1.00237 | 4,824 / 105 | 4,824 / 105 |
| 3 | 8,432 | 8,457 | 1.00296 | 4,824 / 105 | 4,824 / 105 |

The mean same-round all-12 ratio is 1.00304 (SD 0.00071), a 0.30% local
increase versus the prefilter implementation. Allocations are unchanged.
The earlier prefilter sparse ECS sample allocated 4,520 B and 97 objects per
operation; the staged head restores the exact-base 4,192 B and 95 objects.
Its earlier CPU samples overlapped other host work and are not used as timing
evidence.

## Disposition and operator signal

The staged-map candidate is retained: sparse-row allocations match exact
base, the all-12 case does not add allocations, and measured local CPU overhead
is below the 10% investigation threshold in the stated shapes. Classification:
bounded per-row resource result. Endpoint latency and repo-scale throughput
remain unmeasured. The cloud inventory query duration and error telemetry,
with the returned bounded attributes, remain the operator signals; this
change adds no span, metric, log, or status field.

The exact-head golden corpus replay at `6c33e395daa9` exited 0 with 646
passing assertions and zero required failures. Its first drain and maintenance
times carried two advisory warnings (186s and 201s); this benchmark does not
explain or waive those warnings. A subsequent documentation-only commit does
not change the runtime or fixtures, and the replay is identified by its actual
source head rather than presented as proof from a later commit.
