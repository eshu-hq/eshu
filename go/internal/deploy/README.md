# Deploy extraction census

## Purpose

This package owns the `eshu-deploy` extraction census for issue #8871: the
inventory of deployment assets (Helm chart, Compose overlays, GitOps
examples, deployment defaults, compatibility matrix) moving to
`eshu-hq/eshu-deploy`, and the contract tests that pin the census record
to the tree until the cutover lands.

## Ownership boundary

Owns the census record
(`docs/internal/evidence/8871-eshu-deploy-census.md`), the deploy
contract record
(`docs/internal/evidence/8871-eshu-deploy-contract.md`), and their
contract tests only. Does not own the chart, the compose files, the
release pipelines, or the schema-bootstrap implementation — those live
with their respective owners (`deploy/`, `.github/workflows/`,
`go/cmd/bootstrap-data-plane`). After the cutover, the runtime contract
tests in `go/internal/runtime` repoint at the published eshu-deploy
artifact versions.

## Exported surface

None. This is a test-only package; the contract is the census record plus
the `TestExtraction*` tests.

See `doc.go` for the full godoc contract.

## Dependencies

- `gopkg.in/yaml.v3` — parse `values.yaml` and `values.schema.json`
  without duplicating the chart's value gate.

## Telemetry

None. Census tests emit no metrics, spans, or logs beyond `go test`
output.

## Gotchas / invariants

- Every count in the census `## Counts` section is parsed as
  `- <label>: <N>` and compared against the tree; keep that line shape
  exact and keep other list lines away from it.
- The census file is a lockstep docs input: it is registered in
  `specs/ci-docs-lockstep-paths.txt`, `go/internal/cigates/docsset.go`,
  and the three workflow `lockstep` filters.
- The core image uses an explicit moving tag plus release-automation
  promotion (`promote-moving-tags`); only the bundled collector defaults
  are digest-pinned. Do not "fix" the tag policy in the census — record
  it.

## Related docs

- `docs/internal/design/eshu-ecosystem-repository-migration.md`
- `docs/public/deploy/kubernetes/runtime-parity-matrix.md`
- `deploy/helm/eshu/README.md`
