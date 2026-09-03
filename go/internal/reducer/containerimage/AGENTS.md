# AGENTS.md — internal/reducer/containerimage

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `reducer/cicdrun`,
`reducer/packagesourcecore`, `reducer/sbomattest`, `internal/facts`,
`internal/telemetry`, `internal/truth`, and the factschema SDK. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper or an identity derivation goes to `reducer/payloadcore`,
  with a one-line forwarder left in root so existing root callers compile
  unchanged — this is how `ociRepositoryID`/`boolPayload` already worked
  before this package existed, and root still forwards to `payloadcore`
  directly for them today;
- vocabulary (a domain name, a result status, an outcome enum) goes to
  `reducer/contract`, with a root alias;
- a symbol the root genuinely owns as logic AND is still shared by other
  in-root families — `GraphQueryRunner`, `activeRepositoryFactLoader` — is
  declared locally here instead, structurally identical, per
  `internal/reducer/codetaint/graph_ports.go`'s precedent. Never hoist one of
  these unilaterally; that touches packages this family does not own.

Read the root declaration before deciding: a body of
`return payloadcore.PayloadString(...)` is a forwarder and costs nothing to
bypass, while a real implementation needs a deliberate hoist or a local
structural redeclaration.

## `_test.go` exports do not cross package boundaries

A `_test.go` file's exported symbols are visible only to the package's own
test binary (`go test ./internal/reducer/containerimage`), never to another
package importing it normally — not even during that other package's own test
build. This bit two things during the #6061 move:

- **`container_image_identity_cicdrun_cassette_test.go`** used to live at the
  reducer root as `package reducer_test`, reaching root's `_test.go`-only
  `ContainerImageBuiltFromRowsForReplayTest`. It is now `package
  containerimage_test` (external, not internal `package containerimage`) —
  deliberately, because `internal/replay/cassette` transitively imports the
  reducer root, and root imports this package back for its compatibility
  aliases. An internal test file would pull that whole import chain into this
  package's own test build and create a cycle that only `go vet`/`go test`
  catches, not `go build`.
- The root's cross-family tests (`provenance_replay_tombstone_live_test.go`,
  `provenance_edge_submission_metrics_test.go`) need this family's internals
  from OUTSIDE the package, so those internals are exposed as ordinary
  exported functions/methods in non-`_test.go` files
  (`container_image_identity_replay_export.go`,
  `container_image_identity_root_compat_exports.go`) rather than through a
  `_test.go` export. None of them are called by production code — `rg` for
  `ForReplayTest`/`ForTest` suffixes to find them.

## Root test files needed their own local copies

Several still-in-root test files (`cross_scope_readiness_floor_handler_test.go`,
`supply_chain_impact_repository_anchor_ci_run_test.go`, materialization tests
across AWS/GCP/IAM/security-group families) depended on this family's
unexported test doubles before the move
(`fakeWorkloadIdentityExecer`/`decodedBatchedFactRow`, `ciRunFact`/
`ciArtifactFact`, `metricHasAttrs`, `stubContainerImageIdentityFactLoader`,
the three `recording*Writer` doubles). Since Go cannot share unexported test
symbols across a package boundary, root now keeps its own trimmed copies in
`container_image_identity_root_test_doubles_test.go` and
`container_image_identity_batch_insert_test_helpers_test.go` (this package's
own copy). If you rename or reshape a fixture here that a root file's comment
says it mirrors, check whether the root copy needs the same change — nothing
enforces they stay in sync.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md`. The gate checks only that they exist; keeping
  their contents true is on you.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. If your
  file registers no instrument, use a `No-Observability-Change:` marker naming
  the signals that already cover the stage. Do not invent a metric that is
  absent from `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded
  and at the start of their line, in a tracked note. `README.md` here carries
  them; keep them unbolded and line-initial or the gate stops seeing them.
- **`verify-dirgate.sh`** — this directory counts against the per-directory
  cap, and the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv`
  is a monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim must be
  named for its subject — `container_image_identity_compat.go`, not
  `containerimage_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not move `container_image_identity_cicdrun_cassette_test.go` back to an
  internal `package containerimage` test — see the import-cycle explanation
  above.
- Do not add a root forwarder for something only this package's own tests
  use. Root forwarders exist because a specific still-in-root file names the
  symbol unqualified; check `container_image_identity_compat.go`'s own
  comments before adding another one.
- Do not treat `ParsedContainerImageRef`'s `Raw`/`RepositoryKey`/`Tag`/
  `Digest` fields as free-form. `RepositoryKey` in particular is a join key
  other evidence must normalize identically (`NormalizeContainerRepositoryKey`),
  not a display string.
