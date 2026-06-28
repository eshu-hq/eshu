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
which carries the absolute `path`. Recordings driven over an absolute tree are
therefore machine-specific; the round-trip test records to a temp dir and
compares live-vs-replayed in the same process. A committed, portable corpus is
left to the R-5 wiring step (see below).

## R-5 integration status

R-7 ships with its own offline round-trip + provenance-regression Go test
(`acceptance_test.go`, `provenance_test.go`) — that is the load-bearing offline
gate for this flavor. The R-5 offline replay tier (#4107) is not yet on `main`,
so this package is intentionally NOT coupled to it. The clean seam for R-5 to
exercise these fixtures is `parserfixture.Source` (a `replay.Source`) plus
`parserfixture.NewSource(path)`; when R-5 lands, a thin adapter registers the
parser fixtures into that tier with no change to this package's contract.

## Validate

```bash
cd go && go test ./internal/replay/parserfixture/... -count=1
```

## No-Regression Evidence

`Emitter` reads only the local source tree (no network, no credentials).
`Source` performs no parser run, no network call, and no filesystem read beyond
loading the fixture; it holds no shared mutable state beyond a single drained
flag advanced single-threaded per `collector.Service`. The record→replay
round-trip reproduces identical envelopes including provenance for the Go and
HCL parsers; a changed or dropped `SourceURI` is caught by the round-trip and
loader gates (proven failing-capable by a false-green probe). Re-record is
byte-identical (canonical determinism). Verified by
`go test ./internal/replay/parserfixture/... -count=1`.

## No-Observability-Change

No new metrics, spans, or log lines are emitted by this package. Collector-level
telemetry records normally when `Source` is wired through `collector.Service`.
