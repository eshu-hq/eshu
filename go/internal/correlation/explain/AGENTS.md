# AGENTS.md — internal/correlation/explain guidance for LLM assistants

## Read first

1. `go/internal/correlation/explain/README.md` — output format, sort
   contracts, and invariants
2. `go/internal/correlation/explain/explain.go` — `Render`, `renderEvidence`,
   `compareEvidence`; read the full file before changing any sort or format
3. `go/internal/correlation/engine/README.md` — engine.Result shape; understand
   what match counts contain before changing how match-count lines are rendered
4. `go/internal/correlation/model/README.md` — evidence atom field names used
   in rendered output

## Invariants this package enforces

- **DETERMINISM: line order** — the output section order (header, match-count,
  rejection-reason, evidence) is fixed. Changing the order breaks golden tests
  and explain API consumers. The order is encoded in `Render` at `explain.go:13`.
- **DETERMINISM: match-count sort** — match-count rule names are sorted
  ascending by `slices.Sort` in `Render`. Do not change to insertion order or
  priority order.
- **DETERMINISM: rejection-reason sort** — rejection-reason strings are sorted
  alphabetically by `slices.Sort` in `Render`. Do not change to appending order
  (which is controlled by the engine).
- **DETERMINISM: evidence sort** — `compareEvidence` at `explain.go:63` sorts
  `EvidenceAtom` slices by `(ID ascending, SourceSystem ascending, EvidenceType
  ascending)`. Do not add additional sort keys or change the key order without
  updating golden tests.
- **Evidence is cloned before sorting** — `slices.Clone` in `Render` prevents
  mutations to the input engine.Result evidence slice. Do not sort in-place on
  the original.
- **No trailing newline** — `strings.Join(lines, "\n")` does not append a
  final `\n`. Do not add one; callers that need it append it themselves.
- **Render does not re-evaluate** — `Render` is a pure formatting function.
  It does not call admission.Evaluate or engine.Evaluate. It trusts the engine's
  result entirely.

## Common changes and how to scope them

- **Change the header line format** → edit the `fmt.Sprintf` in `Render`
  at `explain.go:13`. Run `go test ./internal/correlation/explain -count=1`.
  Golden tests in `explain_test.go` assert exact header strings; update them.
  Check whether the explain API or MCP layer parses the header; if so, a
  format change is a breaking wire change.

- **Add a new line type after the header** → add the new section in the
  correct position in the `lines` slice in `Render`. Add a sort step if the
  new lines must be ordered. Run `go test ./internal/correlation/explain -count=1`.
  Document the new line type in this README and in
  `go/internal/correlation/explain/README.md`.

- **Change evidence sort order** → edit `compareEvidence` at `explain.go:63`.
  Run `go test ./internal/correlation/explain -count=1`. Update golden tests
  in `explain_test.go` that assert evidence line order. This is an output
  stability change — check whether any explain API consumer relies on
  evidence ordering.

- **Render a new evidence atom field** → edit `renderEvidence` at
  `explain.go:51`. Ensure the field name matches the model.EvidenceAtom struct
  exactly. Run `go test ./internal/correlation/explain -count=1`. This is a
  format change; update golden tests.

## Failure modes and how to debug

- Symptom: explain output is not stable across runs → most likely cause:
  match count map iteration order is non-deterministic. Check that match rule
  names are sorted before building match-count lines in `Render`. Confirm
  rejection reasons are sorted before building rejection-reason lines.

- Symptom: evidence lines are in a different order than expected → check
  `compareEvidence` at `explain.go:63`. The sort key is
  `(ID, SourceSystem, EvidenceType)`. If two atoms have the same ID and
  SourceSystem, they sort by EvidenceType. Confirm the atoms in the test
  fixture have the expected field values.

- Symptom: match-count lines are missing for a rule you expect → match counts
  are populated only for match-kind rules by the engine. Confirm the rule in
  the pack has the match kind. Check that the result passed to `Render` was
  produced by a full `engine.Evaluate` call, not a manually constructed result
  with an empty match counts map.

- Symptom: rejection-reason lines are in the wrong order → reasons are sorted
  alphabetically in `Render`. The reason `low_confidence` sorts before
  `lost_tie_break` and `structural_mismatch` alphabetically; confirm the
  expected order matches alphabetical sort.

## Anti-patterns specific to this package

- **Calling admission or engine from Render** — `Render` is a pure formatter.
  Do not add any evaluation or admission calls. The contract is: engine first,
  render after.

- **Sorting the input evidence in place** — always clone before sorting.
  In-place sorting mutates the evidence slice in the result, which breaks
  callers that retain the result after calling `Render`.

- **Quoting or escaping field values** — current output includes values
  verbatim. If values can contain spaces or special characters and the API
  parses key=value pairs, a format change is needed. Do not silently change
  quoting behavior for existing fields.

- **Changing confidence rendering precision** — `%.2f` is the rendered
  precision. Changing to higher precision changes the wire format of every
  explain response. This is an API contract change.

## What NOT to change without an ADR

- The section order (header / match-count / rejection-reason / evidence) — API
  consumers and golden tests depend on it.
- The evidence sort key (ID, SourceSystem, EvidenceType in `compareEvidence`) —
  changing it changes the order of lines in explain output, which is a wire
  contract for the explain API.
- The confidence format `%.2f` — changing precision changes explain output for
  all callers.
- The `\n` join with no trailing newline — callers may depend on the absence
  of a trailing newline.
