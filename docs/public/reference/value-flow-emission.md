# Value-Flow Emission Gate

Value-flow and taint emission in Eshu is gated by a single environment variable,
`ESHU_EMIT_DATAFLOW`. The gate is **off by default**. When it is off, the
collector snapshot payload is byte-identical to a build without the value-flow
feature, so existing fact contracts are untouched unless a caller explicitly
opts in.

This page documents what the gate controls, the values it accepts, per-language
coverage, the recommended posture for launch copy, and the operational cost of
enabling it. Every claim here is sourced from the collector and parser code that
reads and consumes the gate.

## What the gate controls

The gate is read once per collector process by `LoadEmitDataflowGate`, which
parses the `ESHU_EMIT_DATAFLOW` contract. The result is threaded into the native
snapshotter as `EmitDataflow`, then carried per file into the parser options
applied during snapshot parsing. Each language parser checks
`Options.EmitDataflow` before emitting any value-flow bucket. The snapshotter
also records that the gate ran (`DataflowScanned`) so the collector can emit a
durable `code_dataflow_scanned` marker for one scope generation, even when a
file produced no findings.

When the gate is **on**, the parser attaches the following payload buckets (the
exact set varies by language; see [Per-language coverage](#per-language-coverage)):

| Bucket | Contents |
| --- | --- |
| `dataflow_functions` | Per-function control-flow graphs plus reaching-definition def-to-use edges. |
| `taint_findings` | Intraprocedural source-to-sink taint findings, each carrying a confidence value and provenance. |
| `interproc_findings` | Cross-function taint findings resolved **within a single file**; cross-file and cross-package composition is the reducer's job. |
| `dataflow_summaries` | Durable `summary.Effects` rows for reducer cross-file and cross-package persistence. Emitted only when both repository identity and package import-path metadata are present. |
| `dataflow_sources` | Durable interprocedural source/sink port rows (Go only today), emitted under the same repository-identity and package import-path gate as `dataflow_summaries`. |

When the gate is **off**, none of these keys are added to the payload. Because no
key is added, the snapshot payload is byte-identical to a build without the
feature. This is the load-bearing contract: turning the gate off leaves every
pre-existing fact contract exactly as it was.

## Accepted values

The gate uses an affirmative-only contract. The raw value is trimmed and
lower-cased, then matched against an exact allow-list:

| Truthy (gate ON) | Everything else (gate OFF) |
| --- | --- |
| `1`, `true`, `yes`, `on` | any other value, empty, or unset |

There are exactly four truthy values. Matching is case-insensitive and ignores
surrounding whitespace. Any value not in the list — including the unset/empty
case — leaves the gate off. The default is **off**.

## Per-language coverage

The buckets a language emits depend on the per-language emitter. The table below
reflects what each parser emits behind the gate.

| Language | Status under the gate | Buckets emitted when ON |
| --- | --- | --- |
| Go | Gated value-flow. | `dataflow_functions`, `taint_findings`, `interproc_findings`, `dataflow_summaries`, `dataflow_sources` (the durable `dataflow_summaries` and `dataflow_sources` buckets only when both repository identity and package import path are present). |
| Python | Gated value-flow. | `dataflow_functions`, `taint_findings`, `interproc_findings`. |
| TypeScript / JavaScript | Gated value-flow. | `dataflow_functions`, `taint_findings`, `interproc_findings`. |
| Java | Gated value-flow. <!-- capability-state: id=reachability.java.value_flow state=gated --> | `dataflow_functions`, `taint_findings`, `interproc_findings`, `dataflow_summaries`, `dataflow_sources` (the durable `dataflow_summaries` and `dataflow_sources` buckets only when both repository identity and Java package identity are present). |
| Other languages | Not emitted. | No value-flow buckets are attached by these parsers. |

For Python and TypeScript/JavaScript, the interprocedural resolution is
intra-file only; the import path passed into the interprocedural composer is
empty until package-ownership metadata is available for those ecosystems, while
repository identity is stable and durable. The durable cross-package
`dataflow_summaries` and `dataflow_sources` buckets are therefore emitted only by
Go and Java today, because they require the package import path that only those
parsers currently supply.

For per-ecosystem reachability and confidence posture, cross-reference the
[Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md)
and the [Capability Catalog](capability-catalog.md).

### Go reachability via govulncheck is a separate, always-on path

Do not conflate value-flow emission with Go vulnerability reachability. Eshu
also ingests `govulncheck` JSON reports and normalizes them into source-truth
call-reachability evidence. That path lives in the vulnerability-intelligence
collector and is **not** gated by `ESHU_EMIT_DATAFLOW`. The govulncheck
reachability path is the always-on reachability signal for Go; the
`ESHU_EMIT_DATAFLOW` gate controls the separate, opt-in value-flow and taint
emission described on this page.

## Launch language guidance

This guidance is mandatory for any launch copy, marketing page, or capability
claim that mentions "taint analysis", "value-flow", or "data-flow":

- Any such claim **must reference this gate explicitly.** Value-flow emission is
  a gated, opt-in capability, not a default-on capability.
- The recommended posture is to label the capability **"preview (opt-in via
  `ESHU_EMIT_DATAFLOW`)"**.
- Launch copy must not imply that value-flow or taint findings are produced by
  default. By default no value-flow bucket is emitted at all.

## Decision

Recorded explicitly so launch and product reviewers can cite it:

- Value-flow emission is launch-ready **only** as an explicitly-gated preview
  capability. It is off by default and must not be implied as a default-on, GA
  capability.
- Go's `govulncheck` reachability is the always-on reachability path and is
  independent of the value-flow gate.

## Operational notes

- Enabling the gate increases parser output: every parsed function in a covered
  language contributes a `dataflow_functions` row, and any taint or
  interprocedural findings add further rows. This grows the snapshot payload and
  the downstream fact volume that the reducer must project.
- Keep the gate **off** unless a consumer is actually reading value-flow and
  taint facts. Enabling it for repositories whose facts no one consumes spends
  parse and projection budget for output that is never read.
- The gate is read per collector process from the environment; there is no
  per-repository override in the gate itself.

## Related

- [Environment Variables](environment-variables.md) — the full environment
  contract, including `ESHU_EMIT_DATAFLOW`.
- [Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md)
  — per-ecosystem reachability and confidence posture.
- [Supply Chain Traceability](../supply-chain-traceability.md) — launch hub for
  supply-chain and reachability capabilities.
- Value-flow taint epics #2704 and #2705 track the engine and emission work
  behind this gate.
