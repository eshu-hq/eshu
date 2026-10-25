# #8833 part 1: skeleton redactor and URL userinfo rule

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md),
and the issue body of #8833 is the binding acceptance list. This note holds the
measurements for part 1, which adds the allowlist-skeleton redactor and the
userinfo-only rule for URL-valued keys but binds neither to any definition.
Every figure below was produced on this branch.

## What changed

- `parser.RedactorSkeleton` and `ContentStore.URLUserinfoKeys`, validated by
  `NewRegistry`. The URL key list joins the fingerprint token only when it is
  non-empty, so a #8812 marker token renders as before.
- `redactSkeleton` (`go/internal/collector/git/content/skeleton_redactor.go`)
  and `redactURLKey` (`marker_url_userinfo.go`), dispatched by
  `ApplyContentStore`.
- A `redactor` label on `eshu_dp_collector_content_store_files_total`.
- `contentStoreTally.storedBody` and `storedBytes` return before the map lookup
  when the policy map is empty. That covers an all-`store` registry and, on the
  default registry, every repository with no `.npmrc` (see the governed-default
  rows below).

Neither new mode is bound by any definition, so this part changes no stored
body, registry fingerprint, or freshness hint. The one production non-store
binding on this base is `.npmrc` under `metadata` (#8978). It is unchanged. The
only visible change is that its counter series gains `redactor="none"`.

## Performance Evidence

No-Regression Evidence: the base is `origin/main` at `a4dc8cdda8`, in a detached
worktree. The head is this branch. Host: 16 vCPU arm64 shared by several agents
(load average 12 to 25), Go 1.26.6. Command:
`go test -run '^$' -bench <name> -count=6 -benchtime=200ms` per package, means of
6 runs.

| Benchmark | Base | Head |
| --- | --- | --- |
| `BenchmarkRedactMarker/typical` | 7172 ns, 24 allocs | 7309 ns, 24 allocs |
| `BenchmarkRedactMarker/clean_1MiB` | 11.59 ms, 83891 allocs | 11.44 ms, 83891 allocs |
| `BenchmarkRedactMarker/dense_1MiB` | 108.5 ms, 192276 allocs | 108.0 ms, 192280 allocs |
| `BenchmarkContentStoreUngoverned/snapshot_stage_5000_files` | 8.91 ns | 8.89 ns |
| `BenchmarkContentStoreUngoverned/emit_stage_per_file` | 8.30 ns | 1.83 ns |

The marker redactor is unchanged within run-to-run spread. A definition without
URL keys pays one nil check per line.

The emit-stage row was investigated. A first head run read 9.91 ns against
8.26 ns on base, interleaved base and head over 3 rounds of 3 runs, so it was
not host noise. Neither `storedBytes` nor `storedBody` inlines in either tree
(`go build -gcflags=-m`). The theory was that the nil-map lookup copies the zero
`parser.ContentStore`, which grew by one slice header (24 bytes). A
`len(stores) == 0` return before the lookup measured 1.83 ns in the same
interleaved run, which confirms it. #8812's own note recorded 1.82 ns for this
stage, so the 8.30 ns base cost was already on main before this branch. The
guard brings it back.

On the current base (#8978 binds `.npmrc`, so the default registry is governed
and a repository with no `.npmrc` yields an empty, non-nil map) the guard covers
the common case too. Measured at this head against the same tree with the two
guards replaced by `if false`, 3 interleaved rounds of 3 runs each,
`-benchtime=200ms`, same host:

| Benchmark | Without guard | With guard |
| --- | --- | --- |
| `BenchmarkContentStoreGovernedDefault/emit_stage_per_file_empty_map` | 8.04 to 8.08 ns | 2.34 ns |
| `BenchmarkContentStoreUngoverned/emit_stage_per_file` | 9.81 to 10.00 ns | 1.83 ns |

So a repository with no `.npmrc` goes from about 8.1 ns to 2.35 ns per emitted
file. A repository with an `.npmrc` has a non-empty map and takes the lookup as
before.

Performance Evidence: new code, measured on the head only, means of 6 runs:

| Benchmark | ns/op | MB/s | allocs |
| --- | --- | --- | --- |
| `BenchmarkRedactSkeleton/typical` (8-line tfvars) | 10188 | 13.5 | 27 |
| `BenchmarkRedactSkeleton/clean_1MiB` (allowlisted values) | 42.6 ms | 24.6 | 123395 |
| `BenchmarkRedactSkeleton/dense_1MiB` (a finding every fifth line) | 114.3 ms | 8.9 | 247626 |
| `BenchmarkRedactMarkerURLKeys` (typical body, URL keys listed) | 50602 | 2.8 | 40 |

The skeleton redactor parses every non-empty line, about one allocation per
line, so a clean body costs about 3.7 times the marker's prefiltered clean
body. Governed bodies are capped at `MaxGovernedBodyBytes` (4 MiB). The worst
case is therefore about 0.46 s per pass at the dense rate, run twice per
collection. tfvars files are small configuration files: a typical one costs
about 10 µs. Neither redactor runs until part 2 binds a definition.

## Observability Evidence

Observability Evidence: no new metric. `eshu_dp_collector_content_store_files_total`
gains a `redactor` label (`marker`, `skeleton`, or `none` for `metadata`). The
stream log's per-key counts become `metadata_metadata` and
`redact_<redactor>_<outcome>`. The label lands on a live series:
`{policy="metadata",outcome="metadata"}` is already emitted for every repository
with an `.npmrc` (#8978), and it now carries `redactor="none"`. A `sum by`
query is unaffected; a query that matches the exact label set must add the
label. The two `redactor="marker"` and `redactor="skeleton"` series stay empty
until part 2 binds a definition. At 3 AM, a non-zero
`{policy="redact",redactor="skeleton",outcome="redactor_failed"}` means a tfvars
body fell back to a metadata row, and the WARN log names the paths and the
bounded reason. `TestContentStoreCounterSplitsByRedactorMode` reads the counter
from a real `telemetry.Instruments` and asserts the log holds no credential.
`scripts/verify-telemetry-coverage.sh` passes.

## Accuracy proof

- `TestRedactSkeletonLineRules` pins the record's line table, including the
  credential-in-key and free-text quoted-key cases.
- `TestRedactSkeletonPropertyCorpus` checks 3000 seeded bodies with nine
  credential shapes (AWS key, `sk_live_`, Slack token, PEM block in a comment
  and in a heredoc, base64, hex, a word with symbols, URL userinfo). The line
  shapes include the review's three bypasses: a flagged token wrapped as a
  `var.`/`module.`/`data.` reference, a bare token with `==` padding, and a
  heredoc or block comment opened after other text. Reverting any one of the
  three skeleton fixes (F1 to F3) fails this test and the fuzz seed corpus; the
  fourth fix (F4, the key-list check) is pinned by
  `TestNewRegistryRejectsInvalidModeFields`. The test checks
  that no planted token survives, that the line count holds, that a second pass
  changes nothing, that every stored line fits a grammar the test writes itself,
  and that every raw finding keeps `REDACTED_<kind>` while no other line gains a
  kind marker.
- `FuzzRedactSkeleton` and `FuzzRedactMarkerURLKeys` run the same invariants.
  The 40 to 60 s local runs passed.
