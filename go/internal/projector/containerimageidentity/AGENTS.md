# AGENTS.md — container-image-identity projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after `projectors3.BuildInternetExposureMaterializationReducerIntent`
   and before `cicdruncorrelation.BuildCICDRunCorrelationReducerIntent`.
5. `go/internal/reducer/container_image_identity.go` and its sibling
   `container_image_identity_*.go` files for what the reducer does with the
   intent this package enqueues: the cross-source digest-first join, tier
   ranking, decision classification, retirement, and provenance-edge writes.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `candidateFactKinds` must mirror every kind `triggerFact`'s switch returns
  true for. `FirstAcrossKinds` uses the list to skip kinds the generation
  does not carry before evaluating the predicate — the list exists for that
  pruning, not to change admission. Adding a trigger kind to the switch
  without adding it here silently drops that kind from being found at all.
- The `aws_relationship` branch triggers only when the decoded `TargetType`
  equals `"container_image"`. `TargetType` is `*string` (optional); a nil
  value and a decode error both report false — do not add a third state or a
  default-true fallback.
- The `file`-kind branch is narrow by design: only a Dockerfile (by parsed
  `dockerfile_stages`, declared `language`, or file name) or a tombstoned
  `.github/workflows/*.yml|yaml` path may trigger. Every repository
  generation carries `file` facts, so widening this branch enqueues an
  intent for every source file in the corpus.
- Keep the `Reason` string (`"container image identity evidence observed"`)
  and the `container_image_identity:<scope>` entity key byte-identical. The
  reducer claims one intent per scope generation and reloads the
  generation's facts itself; the root fan-out parity fixture pins these
  values.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`containerImageIdentitySourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- Do not decode a payload field beyond `aws_relationship.TargetType`, and do
  not check a schema version here; the reducer handler owns typed decode for
  its own evidence loads, and schema-version admission stays with root
  projection.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Adding a trigger kind.** Add it to both the `triggerFact` switch and
  `candidateFactKinds`, decide whether it needs its own decode seam or reads
  only envelope fields, and update the child tests plus the root dispatcher
  tests (`../container_image_identity_projection_test.go` and its
  `_dockerfile`/`_cicd`/`_slsa` siblings).
- **Changing the reason string or the entity key.** Both are asserted
  verbatim by the package tests and by the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`); change them
  together.
- **Adding a second decode seam.** Name the file `factschema_decode_*.go` so
  the payload-usage manifest gate discovers it (see Failure modes below),
  and give the wrapper function a name unique across every
  `factschema_decode*.go` file in the repository — the gate scans by
  function body, not file, but the sibling packages' comments record the
  uniqueness requirement explicitly.

## Failure modes

- **The decode seam file name is load-bearing.** The payload-usage manifest
  gate (`scripts/verify-payload-usage-manifest.sh`) discovers
  `factschema_decode_aws.go` by its `factschema_decode*.go` name and the
  `factschema.FactKindAWSRelationship` reference in its body; renaming the
  file or inlining the decode hides the seam from the gate.
- **Route-serves-data registry path citations.** No entry in
  `go/internal/mcp/route_serves_data_registry*.go` cites a projector source
  file for the `container_image_identity` domain — it cites
  `go/internal/query/sbom_attestation_attachments.go` for the one indirect
  `missing_evidence` signal and names the `:ContainerImage` node label in a
  comment, not a `File:` evidence entry (verified with a positive control
  against the `cloudinventory` citations, which DO name projector files). If
  a route is ever repointed to cite this package's file, that creates the
  coupling the sibling extractions warn about: the registry test reads
  cited files by path and fails with `read ...: no such file` on a rename.
- **`go/internal/storage/postgres/cross_scope_producer_readiness.go`** cites
  `candidateFactKinds` by qualified path in a comment explaining why
  `container_image_identity` intents are NOT fully captured by its producer
  map (they also arrive from aws/azure/gcp/git/sbom_attestation scopes).
  Keep that citation in sync with this file's actual path and symbol name.
- **A trigger fact with no `SourceRef.SourceSystem` and no `CollectorKind`**
  yields an empty `SourceSystem` rather than a literal default. That is the
  preserved pre-extraction behavior, not a bug to patch in passing.
- **Root dispatcher tests live outside this directory**, split by topic:
  `../container_image_identity_projection_test.go` (general/OCI/AWS
  relationship/content-entity), `..._dockerfile_projection_test.go`
  (Dockerfile add/edit/non-trigger; the tombstone-removal trigger-level case
  moved into this package's own test file since it called the now-unexported
  `triggerFact` directly), `..._cicd_projection_test.go` (CI/CD artifact and
  deleted-workflow-tombstone), and `..._slsa_projection_test.go` (SLSA
  provenance and signature-verification). A change here can break any of
  them without touching a file in this directory.

## Anti-patterns

- Do not add a package-local source-system helper that duplicates
  `projectorintent.SourceSystem`; the two-tier fallback IS the
  pre-extraction behavior here (unlike the code-taint/interproc families,
  whose single-tier labels must not use it, or observability-coverage's
  three-tier label, which the shared helper cannot represent).
- Do not import the root `projector` package or `codegraphDerefString`. Root
  imports this package to dispatch, and the reverse direction is an import
  cycle; this package keeps its own decode call the way `ec2` and
  `observabilitycoverage` do. Root's own `decodeAWSRelationship` wrapper no
  longer exists — it had this trigger as its only caller and was removed
  with the extraction, so there is nothing there to import even if the
  cycle were not a problem.
- Do not widen the export surface past
  `BuildContainerImageIdentityReducerIntent`. Every sibling family in this
  series exports exactly one builder and no types.

## Changes needing ADR review

- Adding a second decode seam beyond the `aws_relationship.TargetType` read.
- Changing `reducer.DomainContainerImageIdentity`, the candidate-kind set,
  the entity key, or the two-tier source-system label. All are contract
  surface the reducer handler and the fan-out parity fixture assert against.

## Verification

Use TDD. Run the focused child tests, the root dispatcher projection tests,
the root ordered fan-out parity and probe-count tests, the focused
`TestRouteServesDataRegistry` run, package-doc verification, the projector
package tree, the payload-usage manifest gate, and the golden-corpus gates
selected by the changed paths.
