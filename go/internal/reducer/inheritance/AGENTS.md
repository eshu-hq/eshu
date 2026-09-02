# AGENTS.md — internal/reducer/inheritance

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/factload`,
`reducer/payloadcore`, `reducer/sharedintent`, `internal/codeprovenance`,
`internal/facts` and `pkg/log`. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol the reducer root defines, that is a signal
about where the symbol belongs, not a reason to reach upward:

- a generic helper (a payload accessor, a path qualifier, a string dedupe) goes
  to `reducer/payloadcore`, with a one-line forwarder left in root so existing
  root callers compile unchanged;
- an intent-building shape or shared-projection vocabulary (a projection
  context, a refresh intent type, a partition-key derivation) goes to
  `reducer/sharedintent`, with a root alias or forwarder;
- result vocabulary (a status, a SubSignals key) goes to `reducer/contract`;
- a symbol the root genuinely owns as logic — the partition worker, the lease
  and batch-selection machinery, the edge writer dispatch — stays in root, and
  this package does not use it.

Most apparent blockers here are of the first kind wearing a domain filename.
Read the declaration before deciding: a body of
`return payloadcore.SemanticPayloadString(payload, key)` is a forwarder and
costs nothing to bypass, while a real implementation needs a deliberate hoist.

A test that needs the root's worker machinery is telling you it is a root test,
not a family test. `inherits_edge_partition_convergence_test.go` stayed in the
root for exactly that reason: it drives `ProcessPartitionOnce` with an
inheritance fixture, so it is a partition-worker gate, not a family gate.

## The invariants a change here can silently break

- **The per-edge partition key is edge-unique, not file-unique.** It reads
  file-scoped and is anchored on the child path, but `inheritanceEdgeIdentityKey`
  is in the hash on purpose: the partitioned runner deduplicates by
  `(acceptance key, partition key)`, so collapsing two edges onto one key drops
  one of them with no error and no dead letter.
- **`WholeScopePartitionKey` must equal what the #2898 fence reconstructs.** It
  delegates to `sharedintent.RepoWideRetractRefreshPartitionKey` rather than
  minting its own. A key the fence cannot rebuild makes it miss the refresh and
  defer every cross-partition edge forever.
- **Delta scoping is per repository.** Never gate the file-scoped retract on
  `DeltaScope.HasDelta`, which is scope-wide, and never on "this repository has
  qualified paths". `sharedintent.ApplyRepoRefreshDeltaScope` carries both wrong
  readings and the edge loss each causes (#6216).
- **`inheritanceEntityPathKey` is `relative_path`.** Do not add a `path`
  fallback: no `content_entity` fact this repo emits carries a top-level `path`,
  so the fallback would be dead code masking a real ordering bug (#5996).
- **The fact-kind and evidence-source strings are durable wire values.**
  `EvidenceSource` is read back on the graph-write side. Changing one orphans
  what was written under the old name, and a type-identity test cannot catch
  that; pin the literal in a test if you change it.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. It checks only that the three files EXIST, so it passing says
  nothing about whether their contents are still true. Re-read them yourself
  after changing the exported surface or the telemetry.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree needs
  a row in `docs/public/observability/telemetry-coverage.md`, and a row naming a
  file you moved or deleted goes stale. This package registers no instrument, so
  its rows use a `No-Observability-Change:` marker naming the signals that
  already cover the stage. Do not invent a metric that is absent from
  `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded and
  at the start of their line, on an added line in a tracked note. `README.md`
  here carries them; keep them unbolded and line-initial or the gate stops
  seeing them.
- **`verify-dirgate.sh`** — this directory counts against the 40-non-test-file
  cap, and the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv`
  is a monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem is a sibling package's name or that name plus an underscore,
  so no `inheritance_*.go` may exist in the reducer root; a root file about this
  family is named for its subject instead —
  `inherits_edge_partition_convergence_test.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not add a `path` fallback to the content-entity path read. See above.
