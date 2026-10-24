# go.mod split PLAN (no `sdk/go` edits in this worktree)

Goal: `eshu-hq/eshu-collector-package-registry` builds, releases, and deploys
from its own module using supported public contracts, with no core
implementation handles and no shared-table shortcuts (AC8).

## What moves (mechanical, later PR)

- `go/cmd/collector-package-registry/` becomes the destination `main`
  package (binary name unchanged: `eshu-collector-package-registry`).
- `go/internal/collector/registry/package/` (including `runtime/`) becomes
  the destination collector package tree.
- Companion docs move with the code: `EXTRACTION_PREP.md`,
  `EXTRACTION_CLAIM_DESIGN.md`, this plan, and the credential/version
  matrix. `README.md`/`AGENTS.md`/`doc.go` triples are rewritten for the
  new module root.

## What becomes a versioned dependency (never copied)

1. `sdk/go/factschema` payload encoders — consumed at the published module
   version from #6708. The parity tests in `extraction_parity_test.go` pin
   the encoded keys so a version bump that drops one fails fast.
2. Claim/commit/scope surface (`collector`, `workflow`, `scope`, `facts`,
   `packageidentity`, `storage/postgres`, `telemetry`) — consumed through an
   explicit producer-grant interface surface published from core (#6709),
   backed by the standalone template (#6710). Until that surface exists, the
   destination does not split: a premature module with `replace` directives
   into core internals would preserve the very implementation handles AC8
   forbids.
3. The nine fact kinds and `1.0.0` schema versions stay core-declared in
   `go/internal/facts/supply/chain`; the destination references them, and
   only the contract tests assert them.

## Phases

- Phase 0 (this worktree): census, parity contracts, claim design, this
  plan, credential matrix. No module changes.
- Phase 1: physical move into the destination repo as its own module with
  `replace` directives for review only; prove `go build`, focused tests,
  and the parity suite green without core edits.
- Phase 2: cut `replace` directives to published versions (#6707 SDK
  relocation prerequisite); prove independent build/release/deploy using
  only public contracts; rehearse rollback (AC9).
- Phase 3: after production acceptance, remove the superseded in-tree
  implementation (AC10). Never before.

## Guards

- No `sdk/go` edits anywhere in this plan; the SDK is consumed, not forked.
- New files keep the MIT SPDX header and `gofumpt` formatting; package
  triples (`doc.go`, `README.md`, `AGENTS.md`) follow the moved code.
- Watch the dirgate file cap on both sides of the move; the move commit
  carries a `Dirgate-Growth:` trailer if it grows a directory.
