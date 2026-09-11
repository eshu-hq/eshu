# Infrastructure OpenAPI Paths

The OpenAPI 3.0 path fragments for the infra-resource, Kubernetes,
observability-coverage, and secrets/IAM routes.

Layout:

- `routes.go` — `Routes`: `/api/v0/infra/resources/search`,
  `/api/v0/infra/relationships`, `/api/v0/ecosystem/overview`,
  `/api/v0/ecosystem/graph-summary`, `/api/v0/relationships/catalog`, and
  `/api/v0/relationships/edges`.
- `resource_aggregate.go` — `ResourceAggregate`:
  `GET /api/v0/infra/resources/count` and
  `GET /api/v0/infra/resources/inventory`.
- `kubernetes.go` — `Kubernetes`: `GET /api/v0/kubernetes/correlations`.
- `observability_coverage.go` — `ObservabilityCoverage`:
  `GET /api/v0/observability/coverage/correlations`.
- `secrets_iam.go` — `SecretsIAM`: `/api/v0/secrets-iam/posture-summary`,
  `/api/v0/secrets-iam/identity-trust-chains`,
  `/api/v0/secrets-iam/privilege-posture-observations`,
  `/api/v0/secrets-iam/secret-access-paths`, and
  `/api/v0/secrets-iam/posture-gaps`.

None of these fragments import `openapi/schema`.

## Assembly

`openapi/spec.go` concatenates these constants — plus every other
`paths/<family>` leaf and the shared `openapi/schema` and `components`
blocks — into the single JSON document `Spec()` returns. `Routes` and
`ResourceAggregate` are joined as one group; `Kubernetes`, `SecretsIAM`,
and `ObservabilityCoverage` are joined together later, next to the
`service` and `supplychain` families. A dropped or misplaced constant here
silently changes the published wire contract at `/api/v0/openapi.json`,
and only `scripts/verify-openapi.sh`'s route cross-reference would catch
it.

## Related docs

- `docs/public/reference/http-api.md`
