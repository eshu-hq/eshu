# conformance — Agent Instructions

LLM-assistant companion to `README.md`. Read this before editing any file in
`go/conformance/`.

## Read first

- `README.md` — the 5-command contributor flow and the offline data flow.
- `doc.go` — the godoc contract.
- `../internal/goldengate/` — the shared assertion core. The `Evaluate*`
  functions and the `Snapshot` contract live there; this suite imports them.
- `../internal/replay/cassette/` — the credential-free replay `Source` this suite
  drives, and the cassette file format.
- `docs/internal/design/4102-deterministic-replay-framework.md` §8 — the R-10
  packaging design.

## Invariants

- **No forked assertion logic.** The pass/fail decision MUST come from
  `internal/goldengate.Evaluate*`. `Evaluate` here is only a driver that feeds
  the in-memory observation into those shared functions — the exact analogue of
  the in-repo gate's `checkGraph`. Never re-implement a tolerance/correlation/
  property check here; if the assertion semantics need to change, change them in
  `goldengate` so the in-repo gate inherits the same change.
- **Credential-free and Docker-free.** Everything under `go test ./conformance`
  MUST run with no provider credentials, no Postgres, no graph backend, and no
  Docker. The only network/credential step is the optional, operator-run
  `-mode=record` in the README flow, which is never part of the test.
- **Fail loudly, never silently green.** A malformed cassette (missing field,
  absent/duplicate repository, unknown fact kind) MUST return an error from
  `Observe`, and an empty `Report` MUST be treated as failure (it is, via
  `goldengate.Report.Failed`). The regression test
  (`TestEvaluateFailsWhenADirectoryIsDropped`) proves the assertions actually
  bite — keep an equivalent negative test whenever you add an assertion family,
  or the suite can pass while proving nothing.
- **The starter spec is YAML parsed into the shared struct.** `LoadSpec` uses
  `sigs.k8s.io/yaml`, which honours the `json` tags on `goldengate.Snapshot`.
  Do not add a parallel YAML-only struct; the JSON golden snapshot and this YAML
  spec are one contract.
- **The starter tape stays schema-valid.** `testdata/starter-cassette.json` MUST
  validate against the R-3 cassette JSON Schema
  (`TestStarterCassetteIsSchemaValid` guards this). Regenerate it via the
  recorder, not by hand-editing past the schema.
- **`Observe` is the contributor seam.** It is intentionally the one place a
  contributor rewrites for their own fact kinds. Keep it small, documented, and
  free of the assertion logic.

## Tests

```bash
cd go && go test ./conformance -count=1
```

When you add an assertion family to the spec, add: (1) a positive observation
test, (2) a negative test proving the new assertion fails when the observation
regresses, and (3) keep the headline `TestConformance` green against the starter
artifacts.

## Out of scope here

- The live reducer drain, a real graph backend, and HTTP/MCP query truth — those
  need the full pipeline and live in `cmd/golden-corpus-gate` and
  `internal/replay/offlinetier`.
- Changing the assertion semantics (do that in `internal/goldengate`).
