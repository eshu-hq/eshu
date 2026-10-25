# Runtime declaration fact emission (issue #8815)

The git collector now emits `runtime_declaration.version` facts from the
version-pin files. This note records the cost of that emission and why no new
operator signal is added.

No-Regression Evidence:

- **Baseline:** before this change the collector emitted no fact for a pin
  file. The path ran one `NoteCodeownersCandidate` and one
  `NoteSubmoduleCandidate` per content file and nothing for pin names.
- **After:** each content file pays one extra base-name map lookup
  (`NoteRuntimeDeclarationCandidate`). Only the twelve admitted pin names pay
  more, and a repository carries a handful of them. The per-file token scan
  (`eachToken`, which keeps a `{{ ... }}` placeholder as one token and hands
  out substrings without a slice per line) replaced `splitTokens` and the
  line `strings.Split`. On `BenchmarkEmitToolVersions` it gives 866 allocs/op
  and 50.9 to 51.2 KB/op, against 873 allocs/op and 51.7 to 52.0 KB/op before,
  so the scan adds no allocation. ns/op figures from the earlier run moved with
  host load (load average 63), so they are not a speed comparison.
- **Backend and hardware:** no database or graph backend is touched. Go 1.26.10
  on linux/arm64, 16 cores, `go test -bench` with `-count=3`.
- **Input shape and measurement:**

  | Benchmark | Input | Result (3 runs) |
  |---|---|---|
  | `BenchmarkNoteRuntimeDeclarationCandidateNonPin` | 1,024 mixed non-pin paths | 17.17 to 17.28 ns/op, 0 B/op, 0 allocs/op |
  | `BenchmarkEmitToolVersions` | one `.tool-versions` file, 6 lines, 8 tokens | 866 allocs/op, 50.9 to 51.2 KB/op, 8 envelopes; 69.4 to 72.6 us/op (load average 15) |
  | `BenchmarkEmitAtTokenCap` | one `.tool-versions` file at the 1,024-token cap | 120,153 to 120,158 allocs/op, about 6.9 MB/op, 1,024 envelopes; 10.6 to 11.5 ms/op (load average 15) |
  | `BenchmarkEmitOverTokenCapUnderByteCap` | one `.tool-versions` file of 60 KiB (`hostileToolVersions(60<<10)`, 25,205 tokens as the skip reports them), skipped (count only) | 1,587 allocs/op, 171,200 B/op, 0 envelopes; 0.68 ms/op |
  | `BenchmarkEmitOverByteCap` | one `.tool-versions` file of 10 MiB, skipped before parsing | 0 allocs/op, 0 B/op, 0 envelopes; about 27 ns/op |

- **Terminal counts:** the stream tests in
  `go/internal/collector/repo/git/runtime_declaration_facts_test.go` assert the
  emitted `runtime_declaration.version` count and that the stream writer's
  `FactCount` equals the number of envelopes drained: 4 facts for a fixture of three pin files and one `.npmrc`,
  0 facts for a repository with no pin file.
- **Why it is safe:** the emitter is pure parsing over bodies the stream has
  already read, so it adds no file read, no query, no lock, no queue write and
  no graph write. Real pin files are small: the largest realistic one is a
  `.tool-versions` at the token cap, about 10 KiB. A hostile repository can
  commit a pin file of any size, and nothing upstream bounds it (the stream
  reads it with `os.ReadFile`), so the emitter bounds its own cost with the
  byte cap below.
  Candidates are kept in a third map beside the CODEOWNERS and `.gitmodules`
  maps (`runtimeDeclarationCandidates` in `git/fact_builder.go`). A pin file
  over the byte cap is recorded by size only (`Candidate.ByteSize`), so the
  walk never retains a body the emitter would skip.
  The emission order is sorted by path, so the stream stays deterministic.
  A delta generation re-emits only the pin files it changed.

**Per-file caps:** a pin file over 64 KiB (`MaxBytesPerFile`) or with more
than 1,024 tokens (`MaxTokensPerFile`) emits no fact, because a prefix would
pass for the whole file. The caps bound cost and output; they are not a claim
about real files. The only pin files on the evidence host are 11 `.nvmrc`
fixtures of one token each, so no larger real corpus backed the numbers. The
byte cap is 64 KiB because the largest realistic pin file, a `.tool-versions`
at the token cap, is about 10 KiB, so 64 KiB is six times that and one eighth
of the documentation body cap (512 KiB). It is safe because a skipped file is
reported by a WARN log and a legitimate pin file is far below it.

Review finding P2-A measured the cost before the byte cap, with one `EmitFile`
call on a generated 10 MiB `.tool-versions` (about 5,000,000 one-byte tokens),
then skipped by the token cap:

| Case | Before | After |
|---|---|---|
| 10 MiB file, `TotalAlloc` for one call | 1,866,598,056 B, 0.84 s | 0 B |
| 60 KiB file, 25,205 tokens (`skip.TokenCount`), `TotalAlloc` for one call | 9,787,968 B | 171,216 B |
| 100,000-token file (800 KB) | 31,282 allocs, about 40.7 MB, one full parse | 0 allocs, 0 B, byte cap |

Before, `parse` built a declaration for every token before the token cap was
checked, about 178 times the body size. After, the byte cap is the first
check, and past 1,024 tokens the parser counts without storing, with no slice
per line, so the exact `token_count` stays in the log. One token or one line is
bounded by the file cap, because no `raw_spec` can be longer than the file that
holds it. The tests assert allocation ceilings, never wall time; each fails
when its fix is reverted (byte cap check disabled: four tests fail; the
count-only sink reverted to store every token: the 60 KiB allocation test
fails). The candidate map holds a pin body until emission only when the body is
at or under the byte cap (64 KiB at most per pin file); a larger body keeps its
size alone, as the retention section below shows.

Observability Evidence: the new facts flow through the shared fact stream
writer, so the existing per-generation fact count covers them. The cap skip is
the one new signal: the stream logs WARN `git_runtime_declaration_file_skipped`
with `reason` (`byte_cap_exceeded` or `token_cap_exceeded`), `repository_id`,
`scope_id`, `generation_id`, `relative_path` and `byte_size`, plus `byte_cap`
for a byte-cap skip or `token_count` and `token_cap` for a token-cap skip
(`go/internal/collector/repo/git/snapshot_native_helpers.go`). It never logs
file content. It is a log
only: no metric, no label and no closed reason enum change. An operator who
wants to see a repository's declarations reads the fact store by kind
`runtime_declaration.version`; an operator who sees a pin file with no facts
searches the log for that message. The reducer projection (#8816) and the
query tool (#8817) add the read-side signals when they land. A malformed
payload cannot occur on emit, because every field is a plain string, integer
or boolean.

## Retained candidate memory (review finding P2-B)

The content walk used to keep the full body of every pin file at any depth
until emission, with no size check, so a repository of many large pin files
held all of them at once. `NoteRuntimeDeclarationCandidate` now records a body
over `MaxBytesPerFile` as a size only; a body at or under the cap is kept as
before (at most 64 KiB per pin file). A `Candidate` value struct in the
declaration package was the smaller diff than emitting inline, which would
have moved the writer and the skip recorder into the content loop and lost the
sorted emission order. Measured by summing `len(Candidate.Body)` over the
candidate map for 50 pin files of 1 MiB each at different depths
(`TestNoteRuntimeDeclarationCandidateRetainsNoBodyOverTheByteCap`):

| Case | Before | After |
|---|---|---|
| Body bytes retained for 50 x 1 MiB over-cap pin files | 52,428,800 B | 0 B |

The byte-cap skip log keeps `byte_size`: `TestStreamFactsSkipsManyOverCapPinFilesAndLogsEach`
asserts 50 `byte_cap_exceeded` WARN lines, each with `byte_size` of 1 MiB, and
no fact. Retaining the body again fails the retention test.

## Value rule and `.swift-version` (review of #8969)

The review fixes add `.swift-version` and keep a spaced comparator or a range
in one declaration (`values.go`). A value line that holds an operator byte
(`<>=~^|!-`) is scanned twice, once for its shape and once to emit. A line
without one keeps the single scan. The declaration struct gains one `bool`.
Interleaved A/B, same host, three runs each, `-benchtime=1s`, base is the
branch head before the fixes (`2d930a38ff`), after is
`d3fe1ce963`, load average 3.5 at the end of the run:

| Benchmark | Base | After |
|---|---|---|
| `BenchmarkEmitToolVersions` | 67.0 to 70.7 us/op, 51.2 to 51.3 KB/op, 866 allocs/op | 72.8 to 73.1 us/op, 52.5 to 52.6 KB/op, 866 allocs/op |
| `BenchmarkEmitAtTokenCap` | 10.6 to 11.8 ms/op, about 6.9 MB/op, 120,157 to 120,165 allocs/op | 10.7 to 10.9 ms/op, about 6.9 MB/op, 120,156 to 120,159 allocs/op |
| `BenchmarkEmitOverTokenCapUnderByteCap` | 0.66 to 0.67 ms/op, 171,200 B/op, 1,587 allocs/op | 0.77 to 0.78 ms/op, 199,424 B/op, 1,587 allocs/op |

A typical pin file costs about 6 us more and no extra allocation. The 60 KiB
hostile file costs about 0.1 ms more. Its 28 KB extra is the larger
declaration slice. That slice is still bounded by the token cap, not by the
file. Allocation counts are unchanged, and the allocation-ceiling tests still
pass.

## Manifest sources: package.json and go.mod (second PR of #8815)

The git collector now reads the `engines`, `volta` and `packageManager`
sections of every `package.json` and the `go` and `toolchain` directives of
every `go.mod`, at any depth outside a dependency directory. This section
records the cost, the bounds and why no new operator metric is added.

No-Regression Evidence:

- **Baseline:** before this change the content walk paid one base-name lookup
  per file for the pin names and nothing for `package.json` or `go.mod`
  (`NoteRuntimeDeclarationCandidate`). The parser registry already admits both
  files, so no body is read that was not already read.
- **What is added per file:** one switch on the base name for every content
  file, and for a `package.json` or `go.mod` one bounded scan of the body held
  in memory. The scan allocates for no value it skips. A manifest is parsed
  when the walk meets it and the walk keeps only its declarations (a few
  hundred bytes), never the body, so the retained state does not grow with the
  size of the manifests.
- **Backend and hardware:** no database or graph backend is touched. Go 1.26.10
  on linux/arm64, 16 cores, `go test -bench`, load average 4 to 5 during the
  runs. Base is the branch parent (`b46fe6aa1d`), run from a second worktree
  with the same benchmark file, interleaved with the after runs, three rounds.
- **Corpus:** 2,104 real files: 1,371 `package.json` (dependency manifests from
  installed plugin trees and repository fixtures; 376 declare `engines`, 1
  `volta`, 7 `packageManager`; average 858 bytes, largest 23,931) and 733
  `go.mod` (the host Go module cache; 723 have a `go` directive, 27 a
  `toolchain`). Average body 714 bytes. The corpus is a host directory, so the
  benchmark skips unless `ESHU_DECLARATION_CORPUS_DIR` points at one.

  | Benchmark | Base (3 rounds) | After (3 rounds) |
  |---|---|---|
  | `BenchmarkNoteRuntimeDeclarationCandidateCorpus`, whole hook over the corpus | 11.2 to 11.7 ns/file, 0 B, 0 allocs | 1,690 to 1,713 ns/file, 393 B/file, 1.25 allocs/file |
  | `BenchmarkNoteRuntimeDeclarationCandidateNonPin`, a non-pin, non-manifest path | 13.1 to 13.5 ns/op, 0 allocs | 14.5 to 19.9 ns/op, 0 allocs (one noisy round; 14.5 twice) |
  | `BenchmarkEmitToolVersions` (pin path) | 73.6 to 74.2 us/op, 866 allocs | 73.7 to 74.1 us/op, 866 allocs |
  | `BenchmarkEmitAtTokenCap` (pin path) | 10.6 to 11.1 ms/op, about 6.9 MB, 120,140 to 120,154 allocs | 10.7 ms/op, about 6.9 MB, 120,140 to 120,151 allocs |

  For scale, the parser that already runs on the same files costs 37.8 to
  38.7 us/file on this corpus (`json.Parse` and `gomod.Parse`, including the file
  read, `go test -bench` with a scratch benchmark that is not committed). The
  added hook is about 4.4 percent of that, and it reads no file. A repository
  pays it once per `package.json` or `go.mod`; a typical repository holds
  between one and a few hundred.
- **Per-file figures** (`emitter_bench_test.go`, `-count=3`):

  | Benchmark | Input | Result |
  |---|---|---|
  | `BenchmarkExtractPackageJSONNoDeclaration` | the common package.json: no section | 421 ns/op, 0 B/op, 0 allocs/op |
  | `BenchmarkEmitPackageJSON` | engines (2), volta (2), packageManager: 5 facts | 54.2 us/op, 37 KB/op, 610 allocs/op |
  | `BenchmarkEmitGoMod` | go and toolchain beside require and replace | 20.0 to 20.4 us/op, 13.3 KB/op, 229 allocs/op |
  | `BenchmarkEmitGoModLarge` | 400 require lines, 2 facts | 38.7 to 39.2 us/op, 13.3 KB/op, 229 allocs/op (544 MB/s) |
  | `BenchmarkEmitPackageJSONAtByteCap` | valid package.json of about 1,047,600 bytes, 2 facts | 1.82 ms/op, 13.3 KB/op, 237 allocs/op (575 MB/s) |

  The cost of emitting is the envelope build, about 10 us per fact (the same as
  the pin files); the scan is 0.4 us for a manifest with no section and flat in
  the body size. B/op at 1 MiB (13.3 KB for two facts) equals the 13.3 KB of
  `BenchmarkEmitGoMod`, which also emits two facts from a few hundred bytes.
- **Hostile input** (`manifest_hostile_test.go`, ceilings on allocation and a
  generous wall bound, never a speed claim):

  | Input | Result |
  |---|---|
  | 10 MiB body | skipped before parsing, at most 16 KiB allocated |
  | 500,000 nested `[`, 100,000 nested `{"a":`, deep mixed | unparseable at depth 64, at most 32 KiB allocated, no fact |
  | 1 MiB of repeated `"engines"` and `"packageManager"` keys | last one wins, at most 64 KiB allocated |
  | 1 MiB of escaped-key repeats | at most 24 MiB transient (each escaped key decodes into a short string, linear in the body, nothing retained) |
  | `go.mod` with 100,000 `go` lines, 50,000 `toolchain` lines, 100,000 `require (` openers, quote soup, NUL bytes | at most 4 MiB allocated, at most 2 facts, under 2 s |

  The repeated-directive case first allocated 43.7 MB (a `go` line repeated
  100,000 times appended a declaration each); the extractor now keeps the first
  and a count. A first draft held `declaration.sourceKind` as a string and grew
  the pin-file token-cap allocation from 199 KB to 328 KB, tripping
  `TestEmitOverTheTokenCapUnderTheByteCapCountsWithoutStoring`; the section is a
  byte in the struct's padding, so the pin path is unchanged (table above).
- **RED to GREEN:** the 19 package.json and go.mod tests in
  `packagejson_test.go` and `gomod_test.go` fail on the base commit with only
  the new constants stubbed to compile (for example `TestEmitPackageJSONReadsEnginesVoltaAndPackageManager`: "emitted 0 facts, want 5"). Four stream tests in
  `git/manifest_declaration_emission_test.go` fail with the manifest branch of
  `NoteRuntimeDeclarationCandidate` disabled. `FuzzEmitManifestInvariants` ran 4.1
  million executions in 40 seconds with no finding.
- **Terminal counts:** the conformance fixture
  (`TestSnapshotRepositoryRuntimeDeclarationConformanceFixture`) runs the real
  discovery, parser registry and fact stream over a monorepo and asserts the
  exact 14-fact list across 7 source kinds and 5 `scope_dir` values, with
  `FactCount` equal to the envelopes drained. The content-version fixture
  moves from 350 to 351 facts and `gitGenerationContentVersion` from
  `git-collector.v12` to `git-collector.v13`.
- **Why it is safe:** the work is pure parsing over a body already in memory:
  no file read, query, lock, queue write or graph write. Emission order is path
  order, so the stream stays deterministic. A delta generation re-emits only
  the manifests it changed.

Observability Evidence: the facts flow through the shared fact stream writer, so
the existing per-generation fact count covers them. Two log events answer the
3 AM question "why does this `package.json` or `go.mod` have no fact":
WARN `git_runtime_declaration_file_skipped` now also carries `reason`
`manifest_unparseable` (malformed or too deeply nested JSON) and
`byte_cap_exceeded` with `byte_cap` 1,048,576 for a manifest, and WARN
`git_runtime_declaration_values_rejected` with `rejected_count` says values were
dropped by the credential guard while the file's other facts still emitted. Both
carry `repository_id`, `scope_id`, `generation_id`, `relative_path` and
`byte_size`, never file content or the rejected value
(`TestStreamFactsNeverCarriesAManifestCredentialIntoAFactOrALog` plants a
credential-shaped value and checks the facts and the log). A file with no
section, a field this contract does not read, or a non-string value emits
nothing and logs nothing, because it declared nothing. No metric, label or
closed-reason enum changes. The reducer and query signals land with #8816 and
#8817.

Contract Evidence: no schema change. `source_kind` is an open-set string, so
the five new values need no registry, JSON schema or fixture-pack change; the
only edits under `sdk/go/factschema` are doc comments and the README, which
say that `package.json` and `go.mod` name the section rather than the file and
that `order_index` counts declarations on a line.

## Credential guard (manifest values)

`package.json` and `go.mod` are `store`-policy files (#8819), so their bodies
reach the content store as written. The facts add no new exposure only if a
value that is not a version cannot ride into `raw_spec`. The guard
(`safeSpec`, `manifest.go`) admits a value only when it is 1 to 128 bytes of
ASCII from the version and range grammar (letters, digits, space and
`. + * ^ ~ < > = | , - / : @ $ { } _`), holds no run of 16 or more letters,
digits or underscores, and does not contain `://`. The `packageManager` name is
an allowlist (`npm`, `pnpm`, `yarn`, `bun`), and the integrity suffix after `+`
is cut before the check, so the hash never reaches a fact. The seeded cases
(built by concatenation) are a GitHub-style token, an AWS-style key, a
Slack-style token, a JWT, a URL with a password and a version with a token
tail, each in `engines`, `volta`, `packageManager` and the `go` and `toolchain`
directives. A rejected value is dropped and counted, never truncated or masked.

The guard is a heuristic, not a secret scanner, and the documentation says so:
a short word in an alias position (`hunter2`) passes, because an alias is kept
as written and a short word cannot be told from a tag name. It bounds what a
fact can carry; the body stays under the content-store policy.
