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
  (`splitTokens`, which keeps a `{{ ... }}` placeholder as one token) is
  compared with `strings.Fields` through `go test -overlay`: both give 873
  allocs/op and 51.7 to 52.0 KB/op on `BenchmarkEmitToolVersions`, so the
  scan adds no allocation. The ns/op figures moved with host load (load
  average 63), not with the scan, so they are not a speed comparison.
- **Backend and hardware:** no database or graph backend is touched. Go 1.26.10
  on linux/arm64, 16 cores, `go test -bench` with `-count=3`.
- **Input shape and measurement:**

  | Benchmark | Input | Result (3 runs) |
  |---|---|---|
  | `BenchmarkNoteRuntimeDeclarationCandidateNonPin` | 1,024 mixed non-pin paths | 17.17 to 17.28 ns/op, 0 B/op, 0 allocs/op |
  | `BenchmarkEmitToolVersions` | one `.tool-versions` file, 6 lines, 8 tokens | 873 allocs/op, about 51.7 to 52.0 KB/op, 8 envelopes; 71,751 to 136,274 ns/op across two interleaved runs on a host at load average 63 |
  | `BenchmarkEmitAtTokenCap` | one `.tool-versions` file at the 1,024-token cap | 120,400 allocs/op, about 6.9 MB/op, 1,024 envelopes; 25.0 to 30.7 ms/op under the same load |
  | `BenchmarkEmitFarOverTokenCap` | one `.tool-versions` file of 100,000 tokens, skipped | 31,282 allocs/op, about 40.7 MB/op, 0 envelopes; 42.1 to 55.1 ms/op under the same load |

- **Terminal counts:** the stream tests in
  `go/internal/collector/repo/git/runtime_declaration_facts_test.go` assert the
  emitted `runtime_declaration.version` count and that the stream writer's
  `FactCount` equals the number of envelopes drained: 4 facts for a fixture of three pin files and one `.npmrc`,
  0 facts for a repository with no pin file.
- **Why it is safe:** the emitter is pure parsing over bodies the stream has
  already read, so it adds no file read, no query, no lock, no queue write and
  no graph write. Pin files are a few bytes each.
  Candidate bodies are kept in a third map beside the CODEOWNERS and
  `.gitmodules` maps (`runtimeDeclarationCandidates` in
  `git/fact_builder.go`).
  The emission order is sorted by path, so the stream stays deterministic.
  A delta generation re-emits only the pin files it changed.

**Per-file token cap:** a pin file with more than 1,024 tokens
(`MaxTokensPerFile`) emits no fact, because a prefix would pass for the whole
file. The cap bounds output; it is not a claim about real files. The only pin
files on the evidence host are 11 `.nvmrc` fixtures of one token each, so no
larger real corpus backed the number. A 100,000-token file costs one parse
(about 40.7 MB transient, 42 to 55 ms under load) and then emits nothing;
before the cap it emitted 100,000 facts.

Observability Evidence: the new facts flow through the shared fact stream
writer, so the existing per-generation fact count covers them. The cap skip is
the one new signal: the stream logs WARN `git_runtime_declaration_file_skipped`
with `reason=token_cap_exceeded`, `repository_id`, `scope_id`, `generation_id`,
`relative_path`, `token_count` and `token_cap`
(`go/internal/collector/repo/git/snapshot_native_helpers.go`). It is a log
only: no metric, no label and no closed reason enum change. An operator who
wants to see a repository's declarations reads the fact store by kind
`runtime_declaration.version`; an operator who sees a pin file with no facts
searches the log for that message. The reducer projection (#8816) and the
query tool (#8817) add the read-side signals when they land. A malformed
payload cannot occur on emit, because every field is a plain string, integer
or boolean.
