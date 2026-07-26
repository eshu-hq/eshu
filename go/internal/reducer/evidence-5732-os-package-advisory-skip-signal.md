# Evidence: surface os-package advisory target skip count (#5732)

## What changed

`ListOSPackageAdvisoryFactEnvelopes` now returns the number of targets
skipped due to missing required fields as a second return value. The skipped
count flows through the evidence-loading pipeline and surfaces as
`os_package_advisory_targets_skipped` in the reducer's diagnostic signals.

## Why no regression

The skipped counter is a single integer addition to an existing loop with
no new queries or allocations. The B-7 golden gate confirms zero behavioral
change to projected truth (492 pass / 0 required-fail, wall-time 101 s within
baseline variance). The diagnostic signal is a structured-log attribute, not
a registered instrument — zero telemetry overhead.

No-Regression Evidence: goldengate `verify-golden-corpus-gate`
(credential-free cassette replay, 20-repo corpus, NornicDB v1.1.9, 492 pass /
0 required-fail). Wall-time: 101 s (within baseline). No change to projected
truth. Backend: NornicDB v1.1.9. Input: 20-repo cassette corpus. Terminal
queue depth = 0, 492 result assertions passed.

No-Observability-Change: `sub_signal_os_package_advisory_targets_skipped`
is a structured-log field (not an `eshu_dp_*` registered instrument). No new
metric in `go/internal/telemetry/instruments.go`, no X1 row in telemetry
coverage doc, no X4 dashboard change. The skip count is zero in practice
(defensive-only guard; same required fields enforced at persist time).
