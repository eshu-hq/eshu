# IaC OpenAPI Paths

The OpenAPI 3.0 path fragments for the IaC-drift and replatforming routes.

Layout:

- `routes.go` — `Routes`: `/api/v0/iac/dead`, `/api/v0/iac/unmanaged-resources`,
  `/api/v0/iac/terraform-import-plan/candidates`,
  `/api/v0/iac/management-status`, and
  `/api/v0/iac/management-status/explain`.
- `resources.go` — `Resources`: `GET /api/v0/iac/resources`.
- `terraform_config_state_drift.go` — `TerraformConfigStateDrift`:
  `GET /api/v0/terraform/config-state-drift/findings`.
- `replatforming.go` — `Replatforming`: `POST /api/v0/replatforming/plans`.
- `replatforming_ownership.go` — `ReplatformingOwnership`:
  `POST /api/v0/replatforming/ownership-packets`.
- `replatforming_selectors.go` — `ReplatformingSelectors`:
  `GET /api/v0/replatforming/selectors`.
- `replatforming_rollups.go` — `ReplatformingRollups`:
  `GET /api/v0/replatforming/rollups`.

The replatforming fragments are documented here rather than in a package of
their own because every replatforming route is handled by an `IaCHandler`
method (`go/internal/query/replatforming_*_handler.go`) — they are the same
handler family as the drift and resource routes, just a later addition to
it.

None of these fragments import `openapi/schema`.

## Assembly

`openapi/spec.go` concatenates these constants — plus every other
`paths/<family>` leaf and the shared `openapi/schema` and `components`
blocks — into the single JSON document `Spec()` returns; the four
replatforming constants and `Resources`/`TerraformConfigStateDrift` are
joined in their own cluster, separately from `Routes`. A dropped or
misplaced constant here silently changes the published wire contract at
`/api/v0/openapi.json`, and only `scripts/verify-openapi.sh`'s route
cross-reference would catch it.

## Related docs

- `docs/public/reference/http-api.md`
