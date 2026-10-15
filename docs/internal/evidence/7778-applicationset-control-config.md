# #7778 control-config evidence: performance and observability note

Change: `discoverArgoCDDocumentEvidence` and
`discoverStructuredArgoCDEvidence` resolve template sources for generators
that read config from the control repository itself (G==C), emitting one
control-to-deployed template-source fact plus literal-destination platform
facts and one `template_source_control_config` counter add per matched file.

## Measurement

No-Regression Evidence: discovery over 200 synthetic structured G==C
ApplicationSet envelopes (one ApplicationSet each, literal destination),
`BenchmarkControlConfigDiscoveryThroughput`, `GOTOOLCHAIN=go1.26.9 CC=clang`,
Intel i7-8700K, `-benchtime 30x -count 3`, in-memory only (no backend).

| Tree | Median ns/op (200 files) | Per file | B/op | Allocs/op |
|---|---|---|---|---|
| Base 88beb12d0 (skips G==C, zero facts) | 1,058,000 | 5.3 us | 1,068,000 | 6,820 |
| Head (emits template + platform facts) | 2,405,000 | 12.0 us | 2,328,000 | 16,642 |

The delta (+6.7 us/file) is the 400 newly emitted facts and their Details
maps — the fix itself, previously zero output — not slower matching: the
per-file discovery iteration (CSV parse, catalog match) is unchanged, and
per-fact cost matches the existing self-reference path from #7783. Absolute
cost stays at ~12 us/file, dwarfed by surrounding parse, reducer, and graph
write stages. Terminal counts: 200 files in, 200 template-source facts plus
200 platform facts out, stats `ControlConfig: 200`, zero self-loops.

## Operator signal

Observability Evidence: each newly emitted fact counts one
`template_source_control_config` point on
`eshu_dp_argocd_applicationset_template_source_total`, recorded at the
ingestion commit path and asserted per-outcome by
`TestRecordArgoCDApplicationSetTemplateSourceStatsEmitsOnePointPerOutcome`
(five outcomes) and the commit-path metric test. A 3 AM reader sees G==C
files gaining deployment sources as a nonzero rate on that outcome; a
sustained zero with known G==C files present means the extractor regressed.
No new span, log, or dashboard panel: the existing counter, its public
metrics doc row, and the telemetry-coverage row already cover the signal
(`scripts/verify-telemetry-coverage.sh` passes).
