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
