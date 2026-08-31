# AGENTS.md — S3 projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- All three builders (`BuildLogsToMaterializationReducerIntent`,
  `BuildExternalPrincipalGrantMaterializationReducerIntent`,
  `BuildInternetExposureMaterializationReducerIntent`) must keep the shared
  `aws_resource_materialization:<scope>` entity key — do not give any of them
  a family-distinct key; the durable claim gate matches that prefix directly.
- `BuildLogsToMaterializationReducerIntent` decodes the posture payload
  locally (`decodeS3BucketPosture` in `factschema_decode_aws.go`) instead of
  importing root's decode wrapper. Do not add an import of the root
  `projector` package to reuse it — that creates an import cycle, since root
  imports this package to dispatch. Keep this package's decode call local,
  matching `ec2`'s and `internal/reducer`'s own independent decode copies.
  Keep the `factschema.FactKindS3BucketPosture` reference in that function's
  body — `scripts/verify-payload-usage-manifest.sh` AST-scans for it to
  recognize the function as a decode seam; removing it silently drops this
  fact kind's projector-side field-usage tracking.
- All three builders anchor on the earliest matching fact so the reducer
  claim stays stable across reprojections.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run focused child and root S3 tests, ordered fan-out parity,
package-doc verification, the projector package tree, and the golden-corpus
gates selected by the changed paths.
