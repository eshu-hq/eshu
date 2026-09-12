# Cloud OpenAPI Paths

The OpenAPI 3.0 path fragments for the cloud inventory and runtime-drift
routes.

Layout:

- `routes.go` — `Routes`: `GET /api/v0/cloud/resources`.
- `inventory.go` — `Inventory`: `GET /api/v0/cloud/inventory`.
- `runtime_drift.go` — `RuntimeDrift`: `GET /api/v0/cloud/runtime-drift/findings`.
- `aws_runtime_drift.go` — `AWSRuntimeDrift`: `GET /api/v0/aws/runtime-drift/findings`.

None of these fragments import `openapi/schema`; each is a self-contained
JSON object body.

## Assembly

`openapi/spec.go` concatenates these constants — plus every other
`paths/<family>` leaf and the shared `openapi/schema` and `components`
blocks — into the single JSON document `Spec()` returns. `AWSRuntimeDrift`
is joined near the `iac` group, not next to `Routes`/`Inventory`/
`RuntimeDrift`; a dropped or misplaced constant here silently changes the
published wire contract at `/api/v0/openapi.json`, and only
`scripts/verify-openapi.sh`'s route cross-reference would catch it.

## Related docs

- `docs/public/reference/http-api.md`
