# AGENTS.md — EC2 projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildInstanceNodeMaterializationReducerIntent`,
  `BuildInstanceIdentityMaterializationReducerIntent`, and
  `BuildInternetExposureMaterializationReducerIntent` must keep the shared
  `ec2_instance_node_materialization:<scope>` entity key.
  `BuildUsesProfileMaterializationReducerIntent` must keep its own distinct
  `ec2_uses_profile_materialization:<scope>` key — do not collapse it onto
  either node phase's key; the durable claim gate matches both prefixes
  directly.
- `BuildUsesProfileMaterializationReducerIntent` decodes the posture payload
  locally (`decodeEC2InstancePosture` in `factschema_decode_aws.go`) instead
  of importing root's decode wrapper. Do not add an import of the root
  `projector` package to reuse it — that creates an import cycle, since root
  imports this package to dispatch. Keep this package's decode call local,
  matching `internal/reducer`'s own independent copy of the same wrapper.
  Keep the `factschema.FactKindEC2InstancePosture` reference in that
  function's body — `scripts/verify-payload-usage-manifest.sh` AST-scans for
  it to recognize the function as a decode seam; removing it silently drops
  this fact kind's projector-side field-usage tracking.
- All five builders anchor on the earliest matching `ec2_instance_posture`
  fact so the reducer claim stays stable across reprojections.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run focused child and root EC2 tests, ordered fan-out parity,
package-doc verification, the projector package tree, and the golden-corpus
gates selected by the changed paths.
