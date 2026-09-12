# Impact OpenAPI Paths

The OpenAPI 3.0 path fragments for the change- and blast-radius-impact
routes.

Layout:

- `routes.go` — `Routes`: `/api/v0/impact/trace-deployment-chain`,
  `/api/v0/impact/blast-radius`, `/api/v0/impact/change-surface`, and
  `/api/v0/impact/change-surface/investigate`.
- `contract.go` — `Contract`: `POST /api/v0/impact/contracts`.
- `deployment_config_influence.go` — `deploymentConfigInfluence`:
  `POST /api/v0/impact/deployment-config-influence`.
- `exposure.go` — `Exposure`: `POST /api/v0/impact/trace-exposure-path`.
- `rest.go` — `Rest`: `/api/v0/impact/pre-change`,
  `/api/v0/impact/developer-change-plan`, `/api/v0/impact/entity-map`,
  `/api/v0/impact/resource-investigation`,
  `/api/v0/impact/trace-resource-to-code`, and
  `/api/v0/impact/explain-dependency-path`.

`routes.go` and `deployment_config_influence.go` use `K8sResourceLimits`
from this package's own `k8s_resource_limits.go`; `routes.go` additionally uses
`schema.ImpactRuntimeTopologyLimits` and `schema.EvidenceBoundaries`.
`contract.go`, `exposure.go`, and `rest.go` are self-contained.

## Assembly — the deploymentConfigInfluence exception

`openapi/spec.go` concatenates `Routes`, `Contract`, `Rest`, and `Exposure`
directly. `deploymentConfigInfluence` is not in that list: `routes.go`
already folds it in with its own `+ deploymentConfigInfluence +`, so it
reaches the published document once, through `Routes`. The constant is
unexported, so the compiler already refuses the duplicate-it direction:
`spec.go` is a different package and cannot name it. The direction that
stays open is removal — dropping the `+ deploymentConfigInfluence +` line
from `routes.go` silently drops the route from the published document
instead. Either way `scripts/verify-openapi.sh`'s route cross-reference is
the only thing that would catch it — there is no schema-drift test.

## Related docs

- `docs/public/reference/http-api.md`
