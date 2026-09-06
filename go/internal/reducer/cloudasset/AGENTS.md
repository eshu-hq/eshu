# AGENTS.md — internal/reducer/cloudasset

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/gpphase`,
`reducer/factwrite`, `reducer/payloadcore`, and `internal/facts`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a dedupe-and-sort utility) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value) goes to
  `reducer/contract`, with a root alias;
- graph-phase publication (keyspace, phase, publisher interface) goes to
  `reducer/gpphase`;
- batched/single fact-write mechanics go to `reducer/factwrite`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of `return factwrite.Now(now)` is
a forwarder and costs nothing to bypass.

## What must stay conservative

- `DomainCloudAssetResolution` and its `DomainDefinition` catalog entry stay
  in the reducer root (`registry.go`'s `DefaultDomainDefinitions`), not here --
  only the `Handler`/`Writer` types live in this package. Do not "complete"
  the move by relocating the domain constant or catalog entry.
- An intent with no entity keys, or no related scope id after including its
  own `ScopeID`, MUST fail before the writer is invoked. Never call the writer
  with an empty `EntityKeys` or `RelatedScopeIDs`.
- The graph-readiness phase publish MUST happen only after
  `CloudAssetResolutionWriter.WriteCloudAssetResolution` succeeds. Do not
  reorder this -- a published canonical-nodes-committed phase for a
  reconciliation that never landed is a false readiness signal.
- Entity keys and related scope ids MUST go through
  `payloadcore.UniqueSortedStrings` before they reach the stable fact key or
  canonical id derivation, so retries and replays converge on the same
  identity.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`.
- **`verify-telemetry-coverage.sh`** — this move added no row (a rename is an
  `R` diff, not an `A`); do not add one unless you register a new instrument.
- **`verify-dirgate.sh`** — the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet. Never
  hand-edit it or `tools/golangci-lint-dirgate/grandfather.go`; regenerate both
  from the tree.
- **`verify-doc-citations.sh`** — cite this package by symbol or file, never a
  raw `.go:<line>` pointer; a relocation invalidates any line number a doc
  carried for the old root path.

## Do not

- Do not name a new root file after this directory (`cloudasset_*.go`) --
  dirgate refuses a root file whose stem equals or is prefixed by a sibling
  package name.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local copy (`recordingGraphProjectionPhasePublisher`) rather than importing
  the reducer root's `_test.go` scaffolding, which is impossible anyway across
  packages.
