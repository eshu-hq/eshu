# NVD seed-dir exemption: no-regression and observability evidence (#10817)

## Performance Evidence:

Backend: Apple M5 Max (darwin/arm64), Homebrew Go 1.27.4. The shared NVD
section guard (`validateVulnerabilityNVDWindowSection`) runs once per config
load, not per claim or per tick: it walks the top-level keys of one JSON
document and exact-matches at most three short strings. Adding the third
exact key (`nvd_seed_dir`) adds one constant-time string comparison to that
walk. No hot loop, claim path, index, lock, retry, pacing, or serialization
changes anywhere in this lane, so there is no hot path to benchmark: the
measured proof is the unit suite below, which exercises the guard at both
decode sites, including the seeded-violation widening that must fail.

Input shape: top-level config key strings (closed known set of 3, plus
near-miss keys `nvd_seed_dirs`, `nvd_seed`, `nvd_seed_dir_v2`, and case-,
dash-, and space-variants in the workflow test).

## No-Regression Evidence:

Baseline: the two-key allowlist rejected `nvd_seed_dir` and crash-looped
the QA workflow coordinator on the seeded rollout (2026-11-18). After:
`TestValidateNVDSeedDirKeyExemptIsExactAtBothEntryPoints` and
`TestValidateNVDSeedDirKeyDoesNotDecodeValue` (workflow, 109 lines) plus
`TestValidateNVDSeedDirSectionExemptAndStrict` (coordinator, 3/3 subtests,
88 lines) prove the exact key is accepted at both decode sites, near-miss
keys are rejected with all three valid keys named, and the value stays
unread. The seeded-violation widening (allowlist to a `seed`/`nvd`
substring) fails at both sites before the fix and passes after, and was
reverted. Full `go/internal/workflow` and
`go/internal/coordinator/vulnerability` suites green; `go vet` and `gofmt`
clean; `git diff --check` clean. NOT_CHECKED: live QA crash-loop repro
against the exempted build (verdict from code plus the seeded tests).

## Observability Evidence:

No new signals: the guard emits nothing (no counter, span, or log on
accept or reject beyond the returned error the caller already logs), and
this lane adds no instrument, label span, or log line. The coordinator
still logs the activation-load failure it already logged; the exemption
only stops that failure from firing for the exact `nvd_seed_dir` key.
