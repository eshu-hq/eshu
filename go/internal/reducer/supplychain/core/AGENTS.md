# core — agent instructions (issue #6061)

This directory is the supplychain family. It is mid-restructure:
the tree doc (`docs/internal/design/reducer-target-tree.md`) is the real
target; this package is step 2 (supplychain-core, after
packages/correlation) and the code, cloud, workload, and remaining families
still live in the reducer root and must never import this package. The
tree doc's sequencing step 2 defines the done bar; re-read it before
changing anything here.

## Invariants

- Never import the parent reducer package, and never let the shared tier
  (`contract`, `factload`, `factdecode`, `factwrite`, `schemadecode`,
  `crossscope`, `payloadcore`, `supplychainmodel`) import this package. The
  `cicdrun`, `containerimage`, `servicecatalog`, `correlation`, `source`,
  and `securityalert` imports stay one-way (this package reads their
  exported consts, fact kinds, and alert/consumption types; none of them
  imports this package).
- Every change here is behavior-preserving: same intents admitted, same
  facts written, same metrics emitted. Prove it with the focused family
  tests plus the reducer-root callers (`defaults_additive_domains_*`,
  `defaults_handlers.go`, `service.go`) before claiming done.
- New exports are join contracts other families program against. Name them
  for what they return, document who reads them, and keep the bodies
  cut-paste identical to what they replace.
- Test doubles stay family-local: Go test files cannot share unexported
  symbols across a package boundary, so each side keeps its own copy of
  shared fakes and fixtures (see
  `cross_scope_test_doubles_test.go` here, and the
  staying twins in the parent's `*_test_doubles_test.go` /
  `*_test_fixtures_test.go` / `defaults_cross_scope_readiness_wiring_test.go`
  files). Shared test decode for batched versioned writes lives in
  `factwrite/testutil` — extend that leaf instead of copying decoders.
- `telemetry-coverage.md` rows that name files in this package must keep
  pointing at real files with a net-zero row count; the dirgate row for
  `internal/reducer` ratchets DOWN on family moves and never absorbs new
  files to hold this package's count.
