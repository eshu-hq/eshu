# Arbiter ruling: first in-tree caller for #8879

Date: 2026-11-04. Role: arbiter-eshu, via goal-drive subagent in
`goal-01a1a719`. Context: eshu-hq/eshu#8879 (extract eshu-ingester),
gate record eshu-hq/eshu#9382 (arbiter disposition 2026-11-02:
GATE-1 NOT-SATISFIED, freeze on satellite/IaC-pin/deploy/stage/removal;
AUTHORIZED: disabled-by-default in-tree caller wiring plus in-tree
gate-2 completion).

## Verdict

Wire `document/ocr` first (real `tesseract` backend, `preflight/picture`
transitively) from a new disabled-by-default binary
`go/cmd/collector-document-import`; the wiring PR flips neither gate 1
nor gate 2.

## Basis (verified by the arbiter at the ruling worktree)

- Dormancy holds: non-test importers of the six packages outside their
  subtree are zero (only tests plus `telemetry/contract/ocr.go`, a
  constants file).
- OCR gate-2: 2026-11-02 sandbox record on #9382 (4 formats, 5 identical
  runs, canary-leak false, bound assertions). Design 9728 section 6:
  format-list criterion met; "the QA-tier caller is still the open
  residual." Open residuals: table-column rule, native amd64, #9728 open.
- Media gate-2: #9729 open; WER on real speech and cross-arch bit-identity
  NOT_CHECKED.

## Decisions

1. **Capability: `document/ocr`, first and only in this PR.** It is the sole
   capability whose named residual is the caller itself, and its remaining
   gate-2 residuals are closable in-tree (a code rule, an arch run). Media
   is rejected first: an undefined WER threshold is an accuracy question
   wiring cannot close. `document/export` is rejected first: checklist item
   (v) requires gate 2 "with its real engine," export has none, and choosing
   it to dodge the sandbox proof is gate-shopping against #9382's purpose.
   The three `preflight/*` are rejected as heads: they emit no facts, so
   they cannot satisfy checklist item (iv); `preflight/picture` is exercised
   via `picture.Preflight` anyway.
2. **Caller home: new binary `go/cmd/collector-document-import`.** Rejected:
   `go/cmd/ingester` (retained-core assembly; wiring the extractable lane
   into retained core blurs the drawn boundary) and any API/MCP surface.
3. **Disabled-by-default means three independent offs:** the binary is built
   but never deployed (no chart, no IaC, no image-pin change); at startup it
   fail-closes unless an explicit enable env is true AND an artifact-dir env
   is set AND pinned tesseract `BinaryPath`+`BinarySHA256`+model digest are
   set. No silent fallback to the fixture `LocalEngine`.
4. **Artifact source: operator-supplied local directory of reviewed image
   files**, one `ocr.Request` per file, identity from content SHA-256.
5. **No gate flips and no box ticks.** Merged, only checklist item (ii)
   becomes recordable. Gate 1 still needs (iii) IaC and (iv) live execution;
   gate 2 still needs the caller-profile proof with native amd64 and the
   table-column residual disposition on #9382.

## Required proof (imposed on the wiring PR)

Failing tests first; updated dormancy census; no-regression on
`document/...`, `preflight/...`, and `collector` packages; full build;
disabled-start smoke; existing `eshu_dp_ocr_*` signals only; no new or
changed fact kinds; new cmd ships `doc.go`/`README.md`/`AGENTS.md`;
independent review and `make pre-push` by the orchestrator.
