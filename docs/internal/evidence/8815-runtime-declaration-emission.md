# Runtime declaration fact emission (issue #8815)

The git collector now emits `runtime_declaration.version` facts from the
version-pin files. This note records the cost of that emission and why no new
operator signal is added.

**No-Regression Evidence:**

- **Baseline:** before this change the collector emitted no fact for a pin
  file. The path ran one `NoteCodeownersCandidate` and one
  `NoteSubmoduleCandidate` per content file and nothing for pin names.
- **After:** each content file pays one extra base-name map lookup
  (`NoteRuntimeDeclarationCandidate`). Only the eleven admitted pin names pay
  more, and a repository carries a handful of them.
- **Backend and hardware:** no database or graph backend is touched. Go 1.26.10
  on linux/arm64, 16 cores, `go test -bench` with `-count=3`.
- **Input shape and measurement:**

  | Benchmark | Input | Result (3 runs) |
  |---|---|---|
  | `BenchmarkNoteRuntimeDeclarationCandidateNonPin` | 1,024 mixed non-pin paths | 17.17 to 17.28 ns/op, 0 B/op, 0 allocs/op |
  | `BenchmarkEmitToolVersions` | one `.tool-versions` file, 6 lines, 8 tokens | 67,272 to 78,170 ns/op, about 51.7 KB/op, 873 allocs/op, 8 envelopes |

- **Terminal counts:** the stream tests in
  `go/internal/collector/repo/git/runtime_declaration_facts_test.go` assert the
  emitted `runtime_declaration.version` count and that the stream writer's
  `FactCount` equals the number of envelopes drained: 4 facts for a fixture of three pin files and one `.npmrc`,
  0 facts for a repository with no pin file.
- **Why it is safe:** the emitter is pure parsing over bodies the stream has
  already read, so it adds no file read, no query, no lock, no queue write and
  no graph write. Candidate bodies are kept in one map that the stream already
  keeps for CODEOWNERS and `.gitmodules`, and pin files are a few bytes each.
  The emission order is sorted by path, so the stream stays deterministic.
  A delta generation re-emits only the pin files it changed.

**No-Observability-Change:** The new facts flow through the shared fact stream writer, so they are counted
by the existing per-generation fact count. A malformed payload cannot occur on emit, because every field is a
plain string, integer or boolean. An operator who needs to see the facts reads
them from the fact store by kind `runtime_declaration.version`. The reducer
projection (#8816) and the query tool (#8817) add the read-side signals when
they land.
