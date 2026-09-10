# AGENTS.md — internal/reducer/multicloudruntimedrift

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

`internal/projector/cloud/runtime/drift/multi` is a DIFFERENT package; qualify
any cross-reference to it as `projector/cloud/runtime/drift/multi` so a
doc comment in this package cannot be misread as a local self-reference.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/factwrite`,
`reducer/payloadcore`, `internal/correlation/*`, `internal/facts`,
`internal/telemetry`, `internal/truth`, `pkg/log`, and the factschema SDK. It
must **never** import the parent `internal/reducer` package, directly or
transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a nil-collection guard)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value) goes to
  `reducer/contract`, with a root alias;
- batched fact-write mechanics (row shape, statement fragments, chunking,
  `Now`/`CollectorKind`) go to `reducer/factwrite`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of `return factwrite.Now(now)` is
a forwarder and costs nothing to bypass.

## What must stay conservative

- `excludeAWSOwnedRows` MUST run before candidate construction, every time.
  Do not remove it or reorder it after `multicloud.BuildCandidates` -- the
  shared evidence loader's SQL intentionally returns AWS rows too, and this is
  the only thing that keeps AWS findings exclusively `DomainAWSCloudRuntimeDrift`'s.
- The handler MUST NOT emit correlation-engine telemetry
  (`multicloud.RecordEvaluation`) or admitted-finding logs before the durable
  write succeeds. Telemetry follows truth, not the reverse.
- Admitted-finding logs MUST redact the raw cloud identity string through
  `telemetry.SafeResourceLogAttrs`; log only the bounded `drift.pack`/
  `drift.kind`/`drift.provider` attributes.
- The fact identity (`multiCloudRuntimeDriftIdentity`: scope, generation,
  finding kind, canonical uid) MUST stay stable across replays and concurrent
  workers. Do not add a nondeterministic field (a timestamp, a random suffix)
  to it.

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

- Do not name a new root file after this directory (`multicloudruntimedrift_*.go`)
  — dirgate refuses a root file whose stem equals or is prefixed by a sibling
  package name.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local copies (`fakeMultiCloudRuntimeDriftExecer`,
  `decodeBatchedVersionedFactCalls`, `reducerCounterValue`, `counterTotal`,
  `hasAttrs`, `expectedBatchedExecCount`) rather than importing the reducer
  root's `_test.go` scaffolding, which is impossible anyway across packages.
