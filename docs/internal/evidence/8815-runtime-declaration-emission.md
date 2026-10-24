# Runtime declaration fact emission (issue #8815)

The git collector now emits `runtime_declaration.version` facts from the
version-pin files. This note records the cost of that emission and why no new
operator signal is added.

No-Regression Evidence:

- **Baseline:** before this change the collector emitted no fact for a pin
  file. The path ran one `NoteCodeownersCandidate` and one
  `NoteSubmoduleCandidate` per content file and nothing for pin names.
- **After:** each content file pays one extra base-name map lookup
  (`NoteRuntimeDeclarationCandidate`). Only the eleven admitted pin names pay
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
