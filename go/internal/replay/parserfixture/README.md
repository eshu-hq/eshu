# replay/parserfixture

Deterministic record/replay of parser-emitted `facts.Envelope` values, with
provenance, for the offline replay tier (R-7, epic #4102 Phase 2).

## Purpose

Parsers turn source files into `map[string]any` payloads; the Git collector's
fact-emission seam wraps each parsed file into a durable `facts.Envelope` with
provenance (`SourceRef.SourceURI` = the file path, a stable fact key, and a
source-record id). This flavor records those real envelopes over a source tree
and replays them credential-free, so a parser-fact provenance regression (a
dropped or changed `SourceURI`/`SourceRecordID`/line ref) is detectable by an
offline gate without running the parser or shipping a source tree.

It is a sibling of `replay/cassette` (which records live-collector facts). The
two share the canonical serialization core in `replay` (`Canonicalize`).

## The seam recorded at

The flavor records at the genuine production parser-to-envelope seam, not a
re-implementation:

```
parser.Engine.ParsePath(tree, file, ...)   // real parser -> map[string]any payload
   -> collector.ParserFileFactEnvelope(...) // real "file" fact envelope + provenance
        (thin export over the Git collector's unexported fileFactEnvelope)
```

`collector.ParserFileFactEnvelope` is the exported entry point added for this
flavor; it delegates to the same `fileFactEnvelope` the streaming Git fact
builder uses, so the recorded envelope's `FactID` (from `facts.StableID`),
`StableFactKey` (`file:<repoID>:<relativePath>`), and full `SourceRef` are
production-faithful.

## Pieces

- `Emitter` (`emitter.go`) — record side. Walks the tree in sorted order, runs
  the real parser on each registry-recognized file, and yields the real file
  fact envelopes as one `collector.CollectedGeneration`. Reads local files only;
  no network, no credentials.
- `Record` (`recorder.go`) — drains an `Emitter` and writes a canonical fixture.
  Re-recording the same tree is byte-identical (canonical determinism).
- `Source` (`source.go`) — replay side. Loads a fixture and reproduces the same
  envelopes (including provenance) credential-free. Implements `replay.Source`.
- `File`/`Scope`/`Fact` (`format.go`) — the fixture JSON schema and validation.
  `SourceURI` is a required fact field: a fixture that drops provenance is
  rejected at load.
- `portable.go` — the portability seam. `portableize` replaces the absolute
  repository root with a `{{REPO_ROOT}}` sentinel on record; `rehydrate` binds it
  back to the local checkout on replay. This is what makes a committed fixture
  machine-independent.

## Fixture format

```json
{
  "language": "git",
  "schema_version": "1",
  "scope": {
    "scope_id": "parser_fixture:go_comprehensive",
    "source_system": "git",
    "scope_kind": "repository",
    "collector_kind": "git",
    "generation_id": "canonical-generation-<hash(scope_id)>",
    "observed_at": "2000-01-01T00:00:00Z",
    "repo_id": "go_comprehensive",
    "facts": [
      {
        "fact_kind": "file",
        "stable_fact_key": "file:go_comprehensive:basic_functions.go",
        "payload": { "...": "parser output, verbatim" },
        "source_uri": "<absolute file path>"
      }
    ]
  }
}
```

`generation_id` is the canonical value derived from `scope_id`
(`replay.DerivedGenerationID`); the emitter stamps it so the live run's
generation_id already equals its recorded/replayed form (record is a no-op on
it, and live == replayed exactly).

Note: a `file` fact's payload embeds the parser output under `parsed_file_data`,
which carries the absolute `path`, and `source_uri` is the absolute file path.
Recordings driven over an absolute tree are therefore machine-specific. A
committed fixture must instead be portable: record with `RecordOptions.RepoRoot`
so the repository root is tokenized to `{{REPO_ROOT}}`, and replay it with
`NewSourceRehydrated(path, repoRoot)` so the sentinel binds back to the local
checkout and the envelopes match the live parser byte-for-byte. A temp-dir
recording (no `RepoRoot`) keeps absolute paths and replays through `NewSource`.

## Committed fixtures (C-3, #4175)

Every parser in `specs/parser-backing-ledger.v1.yaml` (cloudformation,
dockerfile, hcl, yaml) has a portable committed fixture under
`testdata/fixtures/<parser>.fixture.json`, recorded over a package-local,
parser-focused tree in `testdata/trees/<parser>/`. These back the C-1
replay-coverage manifest's `parser:<name>` surfaces, taking parser coverage to
100% of the ledger.

`committed_fixtures_test.go` is the proof gate (`proof_gate: parserfixture-tests`
in the manifest):

- `TestCommittedParserFixturesAreCurrent` re-records each tree with the live
  parser and asserts it byte-matches the committed fixture, so a parser change
  that drops or mis-attributes a fact shows up as a fixture diff in CI.
- `TestCommittedParserFixturesReplayGreenWithProvenance` replays each committed
  fixture (rehydrated) and asserts the envelopes + `SourceRef` provenance match
  the live parser and that the intended parser's domain extraction ran.
- `TestLedgerCasesMatchSpec` fails if the ledger gains or loses a parser without
  a matching fixture, keeping coverage at 100%.

Regenerate after a deliberate parser change, then review the diff:

```bash
cd go && go test ./internal/replay/parserfixture/ -update-fixtures -count=1
```

## R-5 integration status

R-7 ships with its own offline round-trip + provenance-regression Go test
(`acceptance_test.go`, `provenance_test.go`) — that is the load-bearing offline
gate for this flavor. The R-5 offline replay tier (#4107) is not yet on `main`,
so this package is intentionally NOT coupled to it. The clean seam for R-5 to
exercise these fixtures is `parserfixture.Source` (a `replay.Source`) plus
`parserfixture.NewSource(path)`; when R-5 lands, a thin adapter registers the
parser fixtures into that tier with no change to this package's contract. A
committed, portable fixture exposes `parserfixture.NewSourceRehydrated(path,
repoRoot)` for the same purpose.

## Validate

```bash
cd go && go test ./internal/replay/parserfixture/... -count=1
```

## No-Regression Evidence

`Emitter` reads only the local source tree (no network, no credentials).
`Source` performs no parser run, no network call, and no filesystem read beyond
loading the fixture; it holds no shared mutable state beyond a single drained
flag advanced single-threaded per `collector.Service`. The record→replay
round-trip reproduces identical envelopes including provenance for every
parser-backing-ledger parser (cloudformation, dockerfile, hcl, yaml) plus the Go
demo; a changed or dropped `SourceURI` is caught by the round-trip and loader
gates (proven failing-capable by a false-green probe), and the portability seam
is proven the inverse of itself with a mutation check. Re-record is
byte-identical (canonical determinism), and committed fixtures carry no
machine-specific checkout path. Verified by
`go test ./internal/replay/parserfixture/... -count=1`.

## No-Observability-Change

No new metrics, spans, or log lines are emitted by this package. Collector-level
telemetry records normally when `Source` is wired through `collector.Service`.
