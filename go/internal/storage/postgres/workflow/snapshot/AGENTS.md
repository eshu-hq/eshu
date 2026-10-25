# AGENTS.md — coordinator snapshot summary codec guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../status/summary/AGENTS.md` for the table, the fences, and the writer
   lock this model shares with the other status summary models.
3. `../../workflow_status.go` for `readCoordinatorSnapshot`,
   `ReadCoordinatorSummaryEntries` and `CoordinatorSummarySourceSHA256`, and
   `../../status_read_telemetry.go` for the reader.

## Invariants

- `Decode(Encode(s))` must equal `s` for every field the live read can set,
  including `CollectorTerminalFailures`, which the status JSON excludes. A new
  field on `status.CoordinatorSnapshot` or its element types needs a wire field,
  an Encode and Decode line, a round-trip test, and a bump of `encodingTag`.
- `TestFixtureSetsEveryExportedField` walks the round-trip fixture by reflection
  and fails when a field of any reachable status type is zero in every
  occurrence, so a field added to the status types fails there until the
  fixture and the wire shape cover it (`ExtensionHosted` from #8951 was missed
  once by a hand-built fixture).
- Decode stays strict (unknown section or field, bad timestamp, trailing data,
  not exactly one `claims` entry). A lenient decoder would serve a half-read row.
- The empty sections the live read returns as empty slices decode to empty
  non-nil slices; terminal failures stay nil when none are stored.
- `AdvanceAges` must touch exactly the durations that grow or shrink with the
  clock. Add a new clock-relative duration to it in the same change that adds the
  field. A zero duration stays zero: the live statement clamps an empty set to
  zero.
- Never import the parent `postgres` package from here.
- `SourceSHA256` takes every statement text the snapshot is computed from.
  Adding a statement to `readCoordinatorSnapshot` without adding it to
  `CoordinatorSummarySourceSHA256` lets a rolling upgrade serve a row computed by
  the old statements.
