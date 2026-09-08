# packagecorrelation — agent instructions (issue #6061)

This directory is the package correlation family. It is mid-restructure:
the tree doc (`docs/internal/design/reducer-target-tree.md`) is the real
target; this package is step 1 (packagecorrelation-first) and the
supplychain family still lives in the reducer root and imports this
package one-way. The step-1 packet and the tree doc's sequencing step 1
define the done bar; re-read both before changing anything here.

## Invariants

- Never import the parent reducer package, and never let the shared tier
  (`contract`, `factload`, `factwrite`, `crossrepo`, `sharedintent`,
  `payloadcore`, `admissiondecision`, `packagesourcecore`) import this
  package. The `securityalert` import stays one-way (this package reads its
  exported alert/consumption types; `securityalert` keeps local copies of
  the bridge helpers it needs and never imports this package).
- Every change here is behavior-preserving: same intents admitted, same
  facts written, same metrics emitted. Prove it with the focused family
  tests plus the reducer-root callers (`supply_chain_impact_*`,
  `code_import_*`, `defaults_additive_domains_*`) before claiming done.
- New exports are join contracts other families program against. Name them
  for what they return, document who reads them, and keep the bodies
  cut-paste identical to what they replace.
- Test doubles stay family-local: Go test files cannot share unexported
  symbols across a package boundary, so each side keeps its own copy of
  shared fakes and fixtures (see
  `packagecorrelation_root_test_doubles_test.go` in the parent, and the
  local-copy notes in this package's `_test.go` files). Shared test decode
  for batched versioned writes lives in `factwrite/factwritetest` — extend
  that leaf instead of copying decoders.
- `telemetry-coverage.md` rows that name files in this package must keep
  pointing at real files with a net-zero row count; the dirgate row for
  `internal/reducer` ratchets DOWN on family moves and never absorbs new
  files to hold this package's count.
