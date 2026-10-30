# declaration — collector cost and observability evidence

Measured cost and observability evidence for the runtime-declaration readers.
The operator-facing contract (admitted sources, classification, caps, telemetry
fields) lives in [README.md](./README.md); this note records the benchmark and
review evidence behind it. JVM (Maven, Gradle, sbt) readers reuse the same
manifest caps and collector signals; their admission behavior is documented in
[README.md](./README.md).

## PHP collector cost and signals

Performance Evidence (#9308, 2026-10-28): a five-pair, sequential local
benchmark ran the same snapshot-to-fact-stream harness and the same 424-byte
three-file `php-app` fixture on the branch merge base `6d04851df8` and PHP
commit `7c3f9afff3`. The harness SHA-256 was
`68d9f32f4b19a4b9b49fea70a407c84c1d9984abb460e317b865aa0a2ed163a1`;
each built test binary used Go 1.26.10 on Linux arm64, 16 CPUs, no graph or
Postgres backend. Each round ran the base control then the changed binary with
`-test.benchtime=500ms -test.count=1 -test.benchmem`. Load1 stayed below 3.72,
under the 8.0 quiet-host ceiling. A pre-existing idle query-proof container
remained up, so these local timings are diagnostic rather than a production
throughput or no-regression claim.

| Per three-file snapshot and drained fact stream | Base | PHP commit |
| --- | ---: | ---: |
| Median time | 1,623,100 ns | 1,874,785 ns |
| Observed time range | 1,499,039-1,645,786 ns | 1,776,529-1,977,383 ns |
| Fact count | 21 | 28 |
| `runtime_declaration.version` facts | 0 | 5 |
| Allocated bytes per operation, observed range | 435,336-435,510 | 532,736-536,822 |

The five same-round time ratios averaged 1.172 (sample SD 0.040). The new
`.php-version` admission adds one `file` and one `content` fact; the other five
new facts are the PHP declarations. The baseline did not support these sources,
so the measured extra cost accompanies the intended output. The accepted
Composer scan caps each body at 1 MiB and retains guarded declarations rather
than the body; `TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`
checks the retention boundary. The corpus pin, deterministic digest, and
five-source snapshot stream tests check the changed fact counts and truth.

No-Regression Evidence (#9308 raw-byte identity fix, 2026-10-28): the actual
JSON `Parse` and legacy `ExtractComposerRuntimeEvidence` paths were measured on
the same 180-byte LF `php-app/composer.json` fixture (SHA-256
`374d6651c0573c42d14eb7352eeefd3f34ff715dc67e4da7b5f0a49536ecde10`).
The temporary benchmark harness SHA-256 was
`131a9bba5035b9922816bfebabb7653c0d5ba8789f2293591c3ef83e74b3376d`;
its setup required both PHP values. On Linux arm64, Go 1.26.6, 16 CPUs,
`GOMAXPROCS=1`, each of five rounds ran pre-fix `9f7bb0cceb` then fixed
`16f7b115f2` with `go test ./internal/parser/json -run '^$' -bench
'^BenchmarkComposer(Parse|Extract)LF$' -benchtime=300ms -count=1 -benchmem
-cpu=1` after a 100 ms warmup per binary. The first set was discarded because
an unrelated proof overlapped it. The repeat set ran from 22:26:12 to 22:26:33
UTC with no observed peer at its boundaries, no containers, and maximum sampled
load1 2.01 (the quiet-host ceiling was 8). `Parse` base/head ns/op by round:
32,220/31,943; 31,895/32,522; 31,953/31,812; 31,993/32,073;
31,816/32,028. Medians were 31,953/32,028 ns/op; same-round head/base ratios
averaged 1.0032 (sample SD 0.0110). `Extract` pairs were 13,755/13,607;
13,612/13,714; 13,665/13,529; 13,671/13,939; 13,526/13,772 ns/op;
medians 13,665/13,714, ratio mean 1.0049 (SD 0.0147). Allocations were
unchanged: `Parse` 25,289 B and 390 allocs/op, `Extract` 10,928 B and 200
allocs/op. The difference is within the paired spread. This measures the two
parser paths on an LF fixture; bare-CR correctness is proved by the native
snapshot-to-fact-stream regression, and end-to-end collector time was not
remeasured for this fix.

No-Observability-Change: no metric, span, or log key was added. Existing
`git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` WARN logs report bounded skips with
reason, relative path, byte size, and rejected count without raw values. A
malformed Composer body that remains byte-identical, or one that remains over
the 1 MiB cap, reaches the typed skip WARN without marking the generation
`stream_dropped`; an unchanged clean scope can reproject without reparsing.
After a native parser miss, the collector makes a failed-only bounded read to
classify that skip and rechecks it at emission. A transient read failure,
valid fallback body, or changed body marks `stream_dropped` with the affected
relative path so the next sweep recollects it. The existing pre-parse identity
drop metric and WARN log cover file read failures, not this Composer mismatch
branch. The accepted Composer path makes no additional read.
An under-cap parser miss adds at most two 1 MiB plus one byte reads and one
canonical JSON extraction in that generation; a proven over-cap file needs
only size checks. This cost is confined to failed Composer parses.
Queue and graph counts were not measured in this local parser/collector
benchmark; the live golden-corpus gate owns projection proof.

## Ruby collector cost and signals

Performance Evidence (#9308, 2026-10-29): a paired, sequential local
benchmark ran the same Gemfile-heavy harness against the branch merge base
`ac99a7edde` and this branch at `0f0c2097c2`. The temporary harness
(`BenchmarkRubyGemfileHeavy`, SHA-256
`d29ea60e1bfb350f7c892e802e4ec9151ba1ffb0951735e46a4f7e96d218bd8b`;
removed after the run)
parses one 4,855-byte `Gemfile` (a `source` line, one literal `ruby`
directive, 200 `gem` lines) via `Parse` (`-benchtime=300x -count=3
-benchmem -cpu=1`, `GOMAXPROCS=1`, Go 1.26.6, linux/arm64; no graph backend,
queue, or persisted rows). Bytes and allocs repeat exactly on each side:
base 192,380 B and 3,235 allocs/op; this branch 225,654 B and 4,267
allocs/op (+17.3%, +31.9%: one extra bounded tree-sitter verification
parse). The cost applies only with a candidate `ruby` directive: the same
200-gem Gemfile without it matches the base within 416 bytes and 6 allocs
(192,796 B, 3,241 allocs, 0.42-0.44 ms/op). Host load was not controlled
(load average 24-30), so wall time (base 0.41-2.32 ms/op, branch 5.70-6.31
ms/op) is diagnostic, not a production claim.

No-Observability-Change: no metric, span, or log key was added. The existing
snapshot-stage duration and emitted-fact counters cover this path; the Git
collector's runtime-declaration skip and rejected-value WARN logs identify
invalid input. Bundler bodies stay under the 1 MiB manifest cap and the
collector retains only the few guarded declarations, never the body.

## IaC collector cost and signals

Benchmark Evidence (#9308, 2026-10-29): a paired, sequential local
benchmark ran the same snapshot-to-fact-stream probe harness on the
branch merge base `f6e093ceac` and the IaC lane head. The temporary
harness (`BenchmarkIACFourFileSnapshotFactStream`, SHA-256
`8b8f9fb7fce9db10b93cb247c0ff779c032f8d503e01d01e913a534b8cde9534`;
removed after the run) snapshots the same 142-byte four-file fixture
(`main.tf`, `terragrunt.hcl`, `.terragrunt-version`,
`charts/app/Chart.yaml`) and drains the streamed generation
(`-benchtime=500ms -count=1 -benchmem`, Go 1.26.6, linux/arm64,
16 CPUs; no graph or Postgres backend). Five base-then-head rounds:

| Per four-file snapshot and drained fact stream | Base | IaC head |
| --- | ---: | ---: |
| Median time | 1,676,761 ns | 1,991,895 ns |
| Observed time range | 1,646,892-1,978,597 ns | 1,856,283-2,272,173 ns |
| Drained envelopes per operation | 24.00 | 30.00 |
| `runtime_declaration.version` facts | 0 | 4 |
| Allocated bytes per operation, observed range | 657,011-658,473 | 741,275-742,638 |
| Allocs per operation, observed range | 3,223-3,224 | 4,203-4,204 |

The five same-round time ratios averaged 1.142 (sample SD 0.038). The
new facts are the four IaC declarations pinned by
`TestIACRuntimeNativeHandoff`; the baseline did not support these
sources, so the measured extra cost accompanies the intended output.
Host load was not controlled (load average 8.85-9.28 around the run
window), so these local timings are diagnostic rather than a
production throughput or no-regression claim.

No-Observability-Change: no metric, span, or log key was added. The
existing `git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` WARN logs report bounded
skips with reason, relative path, byte size, and rejected count
without raw values; an over-cap manifest is skipped at the 1 MiB
`MaxManifestBytesPerFile` bound (`SkipReasonByteCap`) and the
collector retains only the guarded declarations, never the body
(`TestIACRuntimeNativeHandoff` fails if private `iac_runtime_evidence`
persists). A changed or unparsable IaC body marks the stream dropped,
exposing the same pre-existing drop signals as the other manifest
readers.

## Rust collector cost and signals

Performance Evidence (#9308, 2026-10-30): a paired, sequential local
benchmark ran the same snapshot-to-fact-stream probe harness on the
branch merge base `0a9445abeb` and the Rust lane head `d2427950ab`. The
temporary harness (`BenchmarkRustSnapshotFactStream`, SHA-256
`174307930177b1e17d02d349aa3757926d257e1e29080c3f50f534d19ae2f357`;
removed after the run) snapshots the same 93-byte one-file fixture (a
`Cargo.toml` with a `[package].rust-version` pin plus one dependency
entry) and drains the streamed generation
(`-test.run='^$' -test.bench=BenchmarkRustSnapshotFactStream
-benchtime=500ms -count=1 -benchmem`, Go 1.26.6, linux/arm64, 16 CPUs;
no graph or Postgres backend). Five base-then-head rounds:

| Per one-file snapshot and drained fact stream | Base | Rust head |
| --- | ---: | ---: |
| Median time | 2,813,285 ns | 2,859,237 ns |
| Observed time range | 2,767,043-2,864,397 ns | 2,841,394-2,914,743 ns |
| Drained envelopes per operation | 18.00 | 19.00 |
| `runtime_declaration.version` facts | 0 | 1 |
| Allocated bytes per operation, observed range | 547,463-548,279 | 570,707-572,059 |
| Allocs per operation, observed range | 2,252-2,253 | 2,427 |

The five same-round time ratios averaged 1.022 (sample SD 0.010). The
new fact is the one `cargo_package_rust_version` declaration pinned by
`TestRustRuntimeNativeHandoff`; the baseline did not support this
source, so the measured extra cost accompanies the intended output.
Load1 stayed between 0.99 and 1.15 across the run window
(2026-10-30T12:30:49Z to 2026-10-30T12:30:58Z), under the 8.0
quiet-host ceiling, so these local timings are diagnostic rather than a
production throughput or no-regression claim.

No-Observability-Change: no metric, span, or log key was added. The
existing `git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` WARN logs report bounded
skips with reason, relative path, byte size, and rejected count
without raw values; an over-cap manifest is skipped at the 1 MiB
`MaxManifestBytesPerFile` bound (`SkipReasonByteCap`) and the
collector retains only the guarded declarations, never the body
(`TestRustRuntimeNativeHandoff` fails if private `rust_runtime_evidence`
persists). A changed or unparsable `Cargo.toml` marks the stream
dropped, exposing the same pre-existing drop signals as the other
manifest readers.

## Swift collector cost and signals

Performance Evidence (#9308, 2026-10-30): a paired, sequential local
benchmark ran the same temporary harness on the branch base `0701a4aed9`
and the Swift lane head. The temporary harness
(`BenchmarkSwiftPackageEmitHead` plus the shared-path
`BenchmarkSwiftVersionPinEmit`, SHA-256
`288dbba06a136e6b1b5d82df6946e4045ab80c38df3400066146828ab27b3f2d`;
removed after the run) emits a 97-byte `Package.swift` body (one
`swift-tools-version` marker plus a minimal manifest) and a `5.9`
`.swift-version` pin through the production `Emit` path
(`-benchtime=1000x -count=3 -benchmem`, Go 1.26.10, linux/arm64;
no graph or Postgres backend):

| Per operation | Base | Swift head |
| --- | ---: | ---: |
| `BenchmarkSwiftPackageEmitHead` envelopes | 0 (not admitted) | 1 |
| `BenchmarkSwiftPackageEmitHead` bytes/allocs | n/a | 6,923-6,994 B, 118 allocs (identical allocs every run) |
| `BenchmarkSwiftVersionPinEmit` bytes/allocs | 6,396-6,433 B, 109 allocs | 6,396-6,478 B, 109 allocs |

The shared pin path keeps identical allocation counts (109) and byte
figures across the pair, so the additive `manifestSwiftPackage` switch
arm and the one `filepath.Base` compare in the Swift `Parse` entrypoint
do not move the cost of files this lane does not read. The new
`Package.swift` path costs about 7.0 KiB and 118 allocs per manifest per
emission at head only; it runs once per changed `Package.swift` per
generation under the 1 MiB manifest byte cap as a single comment-line
scan that retains one detached declaration. Host load stayed between
20 and 27 across the run window (around 2026-10-30T18:55Z), above the 8.0
quiet-host ceiling, so wall time (base pin 9.7-10.2 us/op, head pin
23.3-53.1 us/op, head package emit 22.0-35.7 us/op) is diagnostic
rather than a production throughput or no-regression claim; the
identical allocs and the unchanged-path argument carry the
no-regression case.

No-Observability-Change: no metric, span, or log key was added. The
`swift_tools_version` declaration rides the same content-stream fact
count as the sibling manifest readers; a missing marker is absent (no
candidate), a marker without a value reuses the existing WARN
`git_runtime_declaration_file_skipped` with `manifest_unparseable`, an
over-cap body reuses it with `byte_cap_exceeded`, and a guarded value
reuses WARN `git_runtime_declaration_values_rejected` with the same
labels. The collector retains only the guarded declaration, never the
body (`TestSwiftRuntimeNativeHandoff` fails if private
`swift_runtime_evidence` persists).

## Node collector cost and signals

Performance Evidence (#9308, 2026-10-30): a paired, sequential local
benchmark ran the shared emission benches on the branch base `ca16186fa4`
and the Node lane head `0c83f21047`
(`go test -run '^$' -bench 'BenchmarkEmitPackageJSON$|BenchmarkEmitGoMod$'
-benchtime 1000x -count 3 -benchmem`, Go 1.26.10, linux/arm64, 16 CPUs; no
graph or Postgres backend). A temporary harness measured the two new
bodies through the production `Emit` path at head only
(`BenchmarkNodePackageManagerBun/Deno`, SHA-256
`73794197f3df40f0882b91e7ed8f2d52003a59c5bf50fba885017dac130337bd`;
removed after the run): the 46-byte bun body emits 1 envelope and the
31-byte deno body emits 1 envelope, matching the committed
`lib-common/runtime_node` fixtures (which add one trailing newline each).

| Per operation | Base | Node head |
| --- | ---: | ---: |
| `BenchmarkEmitPackageJSON` median | 61,768 ns | 61,779 ns |
| `BenchmarkEmitPackageJSON` range | 59,886-61,810 ns | 60,270-73,724 ns |
| `BenchmarkEmitGoMod` median | 21,951 ns | 21,896 ns |
| `BenchmarkEmitGoMod` range | 21,797-22,082 ns | 21,610-21,970 ns |
| `BenchmarkEmitPackageJSON` allocs | 620 | 620 |
| `BenchmarkEmitGoMod` allocs | 233 | 233 |
| Node bun body median | n/a (rejected: unlisted name) | 10,763 ns, 114 allocs |
| Node deno body median | n/a (rejected: unlisted name) | 10,824 ns, 113 allocs |

The same-round medians agree to 1.000 (package.json) and 0.997 (go.mod)
with identical allocation counts: the allowlist grows by two names on the
same map-lookup path, so the shared emission cost does not move. The two
new facts ride that unchanged path (`TestEmitPackageJSONPackageManagerStripsTheIntegritySuffix`
pins 1 envelope each, integrity suffix cut, never stored). Load1 stayed
between 10.8 and 13.1 across the run window (2026-10-30T14:57:35Z to
2026-10-30T14:58:18Z), above the 8.0 quiet-host ceiling, so these local
timings are diagnostic rather than a production throughput claim; the
identical allocs and the unchanged-path argument carry the
no-regression case.

No-Observability-Change: no metric, span, or log key was added. The
existing `git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` WARN logs report bounded skips
with reason, relative path, byte size, and rejected count without raw
values; unlisted `packageManager` names are still rejected and counted
(`TestEmitPackageJSONUnlistedPackageManagerIsRejected`). An over-cap
manifest is skipped at the 1 MiB `MaxManifestBytesPerFile` bound and the
collector retains only the guarded declarations, never the body
(`TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`). A changed
or unparsable `package.json` marks the stream dropped, exposing the same
pre-existing drop signals as the other manifest readers.

## Evidence for the Dockerfile and workflow readers (#8818)

No-Regression Evidence: the pin and manifest emission paths are unchanged
apart from additive switch arms for the two new manifest kinds. Benchmarks at
HEAD `30af13f0` against base `8545023c9f` (`go test -run XXX -bench
'BenchmarkEmitPackageJSON$|BenchmarkEmitGoMod$' -benchtime 1000x -count 3`,
linux/arm64): `BenchmarkEmitPackageJSON` 54.3µs to 54.1µs median with 620
allocs on both, `BenchmarkEmitGoMod` 19.7µs to 19.6µs median with 233 allocs
on both. Identical within noise with identical allocation counts.

Performance Evidence: the new readers run once per changed file per
generation under the 1 MiB manifest byte cap, in a single pass, keeping only
the few declarations (heap retention pinned by
`TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`, extended to both
readers). Near-cap (~1 MiB) worst case at HEAD `30af13f0` (same bench
harness, `-benchtime 100x`): Dockerfile 1.27ms/op with 166 allocs, workflow
8.8ms/op with 260 allocs (a `yaml.v3` decode, linear in lines). Realistic
fixtures (a multi-stage Dockerfile with `ARG`, a five-input workflow) emit in
about 90µs combined.

No-Observability-Change: the package logs nothing itself and adds no metric.
Dockerfile and workflow files flow through the existing skip reasons
(`byte_cap_exceeded`, `token_cap_exceeded`, `manifest_unparseable`,
`value_rejected`) on the existing Git collector WARN logs; no new reason
string, log line, or instrument was added.

## .NET collector cost and signals (#9308)

Performance Evidence (#9308, 2026-10-29): paired sequential local benchmarks
ran the same config harness against base `c32b808f32` and HEAD `225e9bcad9`
(`go test ./internal/parser/ -run XXX -bench
'BenchmarkParseConfig/(json|nuget_project)$' -benchtime 100x -count 3
-benchmem`, Go 1.26.10, linux/arm64; no graph backend, queue, or persisted
rows). The `json` 10K-LOC config path the lane leaves alone apart from one
`global.json` filename compare is unchanged: base 2.36-2.41 ms/op with
1,851,982 B/op and 20,076 allocs/op against HEAD 2.367-2.380 ms/op with
1,851,985 B/op and 20,076 allocs/op, identical bytes and allocs on every run.
The `nuget_project` 10K-LOC synthetic `.csproj` (every property unconditional
so the new evidence pass fires on each of the 10,002 lines) moves from base
31.0-31.8 ms/op, ~25.0 MB/op, 289,789 allocs/op to HEAD 42.2-45.7 ms/op,
~29.6 MB/op, 409,796 allocs/op: one extra bounded evidence walk over the
document, no second file read (the parser now keeps the original bytes from
its single source read). A realistic 6-line `App.csproj` probe (temporary
`BenchmarkProbeSmallCsproj`, `-benchtime 1000x -count 3`, removed after the
run) moves from base 8.2-8.6 us/op, 3,928 B/op, 63 allocs/op to HEAD
12.1-12.5 us/op, ~5,850 B/op, 105 allocs/op: about +4 us, +1.9 KiB, and +42
allocs per `.csproj` parse in absolute terms. The new `dotnet_global_json`
reader (HEAD only, 10K-LOC synthetic `global.json` with one SDK pin plus a
member-project list) costs 9.6-9.8 ms/op, 6.73 MB/op, 20,197 allocs/op and
runs once per changed `global.json` per generation under the 1 MiB manifest
byte cap, keeping only the detached SDK declaration. Host load was not
controlled, so wall time is diagnostic, not a production claim; byte and
alloc figures repeat exactly across runs.

No-Regression Evidence: the `json` config path keeps identical bytes and
allocs; the `.csproj` dependency-row output is unchanged (the existing NuGet
parser tests and the content-ledger pin test cover it) and the additive
evidence pass costs +42 allocs on a realistic manifest. The collector keeps
the existing per-file byte and token caps, so an over-cap `global.json` or
`.csproj` still emits no fact and logs the existing WARN
`git_runtime_declaration_file_skipped`.

No-Observability-Change: no metric, span, or log key was added. The
`dotnet_global_json_sdk`, `dotnet_csproj_target_framework`, and
`dotnet_csproj_target_frameworks` declarations ride the same content-stream
fact count as Composer, Ruby, and JVM; malformed or capped manifests reuse
the existing WARN `git_runtime_declaration_file_skipped` and guarded values
reuse WARN `git_runtime_declaration_values_rejected` with the same labels.

## Haskell collector cost and signals

Performance Evidence (#9308, 2026-10-30): paired sequential local benchmarks
ran the shared emission benches and the representative CloudFormation YAML
parse bench on the branch base `b0371c05d54ab19fd03c848d509e55fc506ea81a` and the Haskell lane head
`4dada48751b9cb3c69d759107a8166b319c14dd2` (`go test -run '^$' -bench
'BenchmarkParseCloudFormationTemplateRepresentative$' and `-bench
'BenchmarkEmitPackageJSON$|BenchmarkEmitGoMod$' -benchtime 1000x -count 3
-benchmem`, Go 1.26.6, linux/arm64, 16 CPUs; no graph or Postgres backend).
The CloudFormation bench fixture is itself named `stack.yaml`, so it walks
the new fall-through path: base 2.44-2.62 ms/op with 1,331,477-1,331,504
B/op and 19,380 allocs/op against HEAD 2.52-2.63 ms/op with
1,331,607-1,331,647 B/op and 19,382 allocs/op. `BenchmarkEmitPackageJSON`
keeps 620 allocs on both (base 58.8-62.3 us/op, HEAD 61.5-63.5 us/op) and
`BenchmarkEmitGoMod` keeps 233 allocs on both (base 21.3-21.7 us/op, HEAD
21.2-22.4 us/op). A temporary head-only probe (uncommitted
`BenchmarkProbeStackResolver`, removed after the run) reads the 62-byte
realistic `stack.yaml` in 7.9-8.3 us/op with 9,016 B/op and 72 allocs/op,
once per changed file per generation under the 1 MiB manifest byte cap.
Sampled load1 sat between 10.4 and 18.3 across the run window
(2026-10-30T18:23Z to 2026-10-30T18:32Z), above the 8.0 quiet-host ceiling,
so wall time is diagnostic; the identical allocation counts and the
single-decode argument carry the no-regression case.

No-Observability-Change: no metric, span, or log key was added. The
`stack_yaml_resolver` declaration rides the same content-stream fact count
as the sibling manifest readers; a malformed or capped `stack.yaml` reuses
the existing WARN `git_runtime_declaration_file_skipped` and a guarded
resolver value reuses WARN `git_runtime_declaration_values_rejected` with
the same labels.

## .NET token-cap alignment (#9548 P2-1)

No-Regression Evidence (#9548 P2-1): aligning the .NET evidence cap with the
shared RuntimeVersionTokenCap (1024) keeps the same bounded-memory contract
as the sibling JVM/Ruby/Python lanes: the parser counts without retaining
past the cap and the collector still emits no fact for an over-cap file. No
new allocation path was added; the realistic-manifest cost profile above is
unchanged.

No-Observability-Change (#9548 P2-1): no metric, span, or log key was added.
The over_tokens skip keeps the existing WARN
`git_runtime_declaration_file_skipped` with the same labels; only the
TokenCount it carries becomes the measured count instead of a hardcoded
constant.

## Perl collector cost and signals (#9308)

Performance Evidence (#9308, 2026-10-30): a paired, sequential local
benchmark ran the shared emission bench on the branch base `0701a4ae` and
the Perl lane head `fa73890f`
(`go test ./internal/collector/repo/runtime/declaration/ -run '^$' -bench
'^BenchmarkEmitPackageJSON$' -benchtime=300ms -count=3 -benchmem`, Go
1.26.6, linux/arm64, 16 CPUs; no graph or Postgres backend): base
67,383/100,098/97,160 ns/op against head 91,977/77,799/68,393 ns/op, with
620 allocs/op and ~37 KB/op on every run of both. A temporary harness
measured the two new bodies through the production `EmitFile` path at head
only (removed after the run): the 27-byte cpanfile emits 1 envelope at
19,355-37,735 ns/op and 121 allocs/op, and the 7-byte `.perl-version`
emits 1 envelope at 22,574-42,781 ns/op and 115 allocs/op.

No-Regression Evidence: the shared package.json path keeps identical allocs
with timings inside the same host-noise band; the new readers run only for
the two new file names (exact-name registry dispatch plus one manifest
switch case), so every other input takes the same code path plus one map
entry and one failed string compare. The collector keeps the existing
per-file byte and token caps, so an over-cap cpanfile emits no fact.

No-Observability-Change: no metric, span, or log key was added. Capped and
guarded cpanfile values reuse the existing WARN
`git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` with the same labels.

## Dart collector cost and signals (#9308)

Performance Evidence (#9308, 2026-10-30): a paired, sequential local
benchmark ran the same snapshot-to-fact-stream probe harness on the
lane base `0701a4aed9` and the Dart lane head. The temporary harness
(`BenchmarkDartTwoFileSnapshotFactStream`; removed after the run)
snapshots the same 74-byte two-file fixture (`pubspec.yaml` with an
`environment.sdk` constraint, `.fvmrc` with a `flutter` pin) and drains
the streamed generation (`-benchtime=500ms -count=1 -benchmem`,
Go 1.26.10, linux/arm64, 16 CPUs; no graph or Postgres backend). Five
base-then-head rounds:

| Per two-file snapshot and drained fact stream | Base | Dart head |
| --- | ---: | ---: |
| Median time | 1,458,548 ns | 1,612,584 ns |
| Observed time range | 1,398,944-6,799,800 ns | 1,523,770-8,841,141 ns |
| Observed time range without the host-noise round | 1,398,944-1,508,477 ns | 1,523,770-1,733,688 ns |
| Allocated bytes per operation | 542,000-545,524 | 623,444-631,070 |
| Allocs per operation | 2,081-2,083 | 2,753-2,758 |
| `runtime_declaration.version` facts | 0 | 2 |

The five same-round time ratios averaged 1.153 (1.117 without round 4,
when host load spiked both trees about fivefold). The new facts are the
two Dart declarations pinned by `TestDartRuntimeNativeHandoff`; the
baseline supports neither source, so the measured extra cost accompanies
the intended output. Host load was not controlled (load average about 9.2
on 16 CPUs around the run window), so these local timings are diagnostic
rather than a production throughput or no-regression claim.

No-Regression Evidence: the per-file byte and token caps are unchanged
(the 1 MiB `MaxManifestBytesPerFile` manifest cap, the shared
`RuntimeVersionTokenCap`), so an over-cap `pubspec.yaml` or `.fvmrc`
still emits no fact and the existing WARN
`git_runtime_declaration_file_skipped` fires with the same labels;
`TestDartManifestByteCap` pins both. Dependency-row output is unchanged
(the existing pubspec and JSON parser tests cover it).

No-Observability-Change: no metric, span, or log key was added. The
`dart_pubspec_environment_sdk` and `dart_fvmrc_flutter` declarations
ride the same content-stream fact count as the sibling lanes; malformed
or capped manifests reuse the existing WARN
`git_runtime_declaration_file_skipped` and guarded values reuse WARN
`git_runtime_declaration_values_rejected` with the same labels.
