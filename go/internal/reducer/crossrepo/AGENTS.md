# AGENTS.md — internal/reducer/crossrepo

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/sharedintent`, `internal/environment`,
`internal/ghactionsref`, `internal/relationships`, `internal/telemetry` and
`pkg/log`. It must **never** import the parent `internal/reducer` package,
directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a slice conversion, a payload accessor, a nil-guard) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root so existing root
  callers compile unchanged;
- vocabulary (a domain name, a fact-kind name, an enum, an outcome value) goes
  to `reducer/contract`, with a root alias;
- a symbol the root genuinely owns as logic stays in root, and this package does
  not use it.

Every blocker the #6061 move surfaced here was the first kind wearing a domain
filename -- `SharedProjectionIntentRow` was a type alias for `sharedintent.Row`,
`GraphProjectionPhaseKey` for `gpphase.PhaseKey`, `toStringSlice` a body of
`return payloadcore.ToStringSlice(value)`. Read the declaration before deciding:
a forwarder costs nothing to bypass, while a real implementation needs a
deliberate hoist.

## The evidence-source constant is a cross-family contract

`CrossRepoEvidenceSource` is exported and read by the Ifá assert gate, because
workload materialization writes the identical
`(WorkloadInstance)-[:RUNS_ON]->(Platform)` shape under a different
`evidence_source`. Relationship type and endpoint labels cannot tell the two
families' edges apart, so that constant is the only thing that can.

Do not copy its literal into a gate, a test, or another package. Reference the
constant, so the assertion cannot drift from what the writer stamps. The same
applies to any new stamped provenance value with a reader outside this package:
give it a name here and let readers reference it.

## Adding an evidence kind

`evidenceKindToType` and `evidenceKindToSourceTool` in
`cross_repo_evidence_type.go` are read by `scripts/verify-edge-source-tool-coverage.sh`,
which parses this file directly rather than calling into it. That gate's path
list lives in `specs/ci-gates.v1.yaml` and
`.github/workflows/static-contract-gates.yml`. If you move or rename that file,
repoint all three or the gate silently checks nothing.

Both maps are keyed on `relationships.EvidenceKind` values, and the derived
`evidence_type` / `source_tool` tokens are durable values written onto graph
edges. Changing one orphans every edge already stamped with the old token, and a
map-shape test cannot catch that. If you change a token, pin the literal in a
test.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. Deleting one fails the build. The gate checks only that the
  three files exist, not that they are true; keeping them accurate is on you.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree needs
  a row in `docs/public/observability/telemetry-coverage.md`, and every existing
  row must name a path that still exists. If your file registers no instrument,
  use a `No-Observability-Change:` marker naming the signals that already cover
  the stage. Do not invent a metric that is absent from
  `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded and
  at the start of their line, on an added line in a tracked note. `README.md`
  here carries them; keep them unbolded and line-initial or the gate stops
  seeing them.
- **`verify-dirgate.sh`** — this directory counts against the 40-file cap, and
  the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv` is a
  monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.
- **`verify-edge-source-tool-coverage.sh`** — see "Adding an evidence kind"
  above.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim must be
  named for its subject — `cross_repo_compat.go`, not `crossrepo_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not drop the retraction path. A source repo that resolves to no edges emits
  retraction intent rows on purpose; returning early instead leaves a stale edge
  standing in the graph, which reads as a live dependency that no longer exists.
- Do not move the activation call above the intent commit. The ordering in
  `Resolve` is the publish fence: the generation activates only after its
  graph-acceptance intents are durably committed, and
  `eshu_dp_cross_repo_activation_fenced_total` is the only signal an operator
  gets when that commit fails.
- Do not add a relative path to a fixture without counting the directory depth.
  This package sits one level deeper than the reducer root, and
  `cross_repo_source_tool_snapshot_test.go`'s golden-snapshot path needed a
  fourth `..` after the move.
