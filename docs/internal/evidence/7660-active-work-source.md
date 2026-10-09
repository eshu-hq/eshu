# #7660 active-work source marker on bundle, causality, control-plane

## Scope

The queue, stage, backlog, blockage, and failure figures on three
operator-facing shapes came with no statement of which reader produced
them: the stored status summary or the live statement. #7660 carries
the `status.ActiveWorkSource` the reader already reports onto the live
evidence bundle (`active_work_source`, omitempty), the freshness
causality map, and the operator control-plane map, all through the one
shared `ActiveWorkSourceJSON` wire shape and the one shared OpenAPI
component.

## Behavior

- `liveEvidenceSnapshotFromReport` copies `report.ActiveWorkSource.JSON()`
  (nil when the reader reports no source).
- The freshness causality and operator control-plane handlers wrap their
  existing maps with `withActiveWorkSource` (one map insert, no key when
  the source is empty).
- `BuildLiveBundle` copies the snapshot pointer onto `Bundle`.
- No SQL, Cypher, I/O, lock, loop, retry, or queue path is touched. Demo
  bundles and marker-less callers serialize byte-identical output: the
  bundle key is `omitempty` and the wrappers add no key for an empty
  source.

Proof: `TestBuildLiveBundleCarriesTheActiveWorkSource`
(evidencebundle) pins present/absent/omitted on the bundle;
`TestLiveEvidenceBundleCarriesTheActiveWorkSource`,
`TestFreshnessCausalityAndControlPlaneCarryTheActiveWorkSource`, and
`TestFreshnessCausalityAndControlPlaneOmitTheActiveWorkSourceWhenTheReaderReportsNone`
(query) pin the marker on the live snapshot, the causality map, and the
control-plane map, and that an empty source leaves the payloads
unchanged; `TestOpenAPIDocumentsTheActiveWorkSourceOnThe7660Routes`
pins the shared OpenAPI component.

## Measurement

Benchmark Evidence: scratch `go test -bench` (not committed), same
machine, same input shape, three 1 s runs each. Input: a synthetic
`status.Report` carrying a `stored_summary` marker
(`AsOf` 2026-10-09T12:00:00Z, `Age` 1.5 s) with `repoCount` 5, the
marker-present worst case (the empty-source case returns nil before the
`time.Format`). Machine: linux/amd64, AMD EPYC 9R14, go 1.26.6. No
database backend: the touched code is an in-memory mapping under routes
whose cost is their millisecond-scale store reads.

| Case | Baseline (`origin/main` 5f72790f6e76) | After (this branch) |
| --- | --- | --- |
| `liveEvidenceSnapshotFromReport` | 233.2, 194.4, 193.7 ns/op | 286.0, 284.0, 283.6 ns/op |
| `withActiveWorkSource` (new) | n/a (no equivalent) | 114.4, 112.8, 115.0 ns/op |

The figures were measured on the pre-amend branch tree; the amend that
added this note changed no Go file, so they stand for the pushed tree.

No-Regression Evidence: the snapshot costs ~90 ns/op more, exactly one
`ActiveWorkSource.JSON()` call (one small struct alloc, one
`time.Format` RFC3339Nano, one `Duration.Seconds()`); the wrapper costs
~114 ns/op absolute (the same conversion plus one map insert). Terminal
queue and row counts are unchanged: no statement is added or altered,
and the shape tests above pin the only wire delta to the new optional
key. The routes this feeds already spend milliseconds on store reads,
so ~0.1 us per request is noise, not a regression.

Observability Evidence: the marker itself is the new operator signal.
An operator doubting a queue/backlog figure reads `active_work_source`
(`source`, `reason`, `as_of`, `age_seconds`, `stale`) on the bundle,
causality, or control-plane payload to see whether the stored summary
or the live statement answered and how old it was. Status evidence is
the passing marker tests named above; no new span, metric, or log was
needed because no new I/O exists to observe.
