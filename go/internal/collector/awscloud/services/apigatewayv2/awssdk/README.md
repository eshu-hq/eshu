# API Gateway v2 AWS SDK Adapter

## Purpose

`internal/collector/awscloud/services/apigatewayv2/awssdk` adapts AWS SDK for Go
v2 API Gateway v2 control-plane responses into the scanner-owned metadata model
defined by the parent `apigatewayv2` package. It pages read-only list operations
and maps SDK shapes into safe metadata records.

## Ownership boundary

This package owns API Gateway v2 SDK pagination, response mapping, and per-call
telemetry. It does not own fact envelope shaping, relationship logic, or
registration. Fact shaping lives in the parent `apigatewayv2` package;
registration lives in the sibling `runtimebind` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the API Gateway v2 SDK adapter implementing `apigatewayv2.Client`.
- `NewClient` - builds an adapter for one claimed AWS boundary.

The accepted SDK surface is the unexported `apiClient` interface. A reflection
test asserts it cannot reach the OpenAPI export, integration/route response
readers, model/template readers, or mutation operations.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/apigatewayv2` and its `types` package.
- `internal/collector/awscloud` for the boundary and shared API-call telemetry.
- `internal/collector/awscloud/services/apigatewayv2` for the scanner-owned
  types.
- `internal/telemetry` for spans and instruments.

## Telemetry

Each AWS operation runs inside `recordAPICall`, which starts the
`aws.service.pagination.page` span and records `eshu_dp_aws_api_calls_total` and
`eshu_dp_aws_throttle_total` with the bounded AWS collector labels.

## Gotchas / invariants

- Allowed API calls are `GetApis`, `GetStages`, `GetRoutes`, `GetIntegrations`,
  `GetAuthorizers`, `GetVpcLinks`, `GetDomainNames`, and `GetApiMappings`. Do not
  add `ExportApi`, `GetIntegrationResponse(s)`, `GetRouteResponse(s)`,
  `GetModel(s)`, `GetModelTemplate`, `ReimportApi`/`ImportApi`, or any mutation
  call without a new issue and an evidence note.
- The mappers never copy `Route.RequestModels`, `Route.RequestParameters`,
  `Route.ModelSelectionExpression`, `Integration.RequestTemplates`,
  `Integration.RequestParameters`, `Integration.ResponseParameters`,
  `Integration.TemplateSelectionExpression`, `Integration.CredentialsArn`,
  `Authorizer.AuthorizerUri`, `Authorizer.AuthorizerCredentialsArn`, or
  `Authorizer.IdentityValidationExpression`. The scanner-owned types have no
  field for them, proven by a struct-reflection guard on the mapped values.
- Pagination follows `NextToken` and stops on nil or empty; treating empty as
  continuation would loop forever. Nil pages are guarded.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/apigatewayv2/awssdk/...`
covers paginated API discovery, per-API stage/route/integration/authorizer
listing, VPC link, domain, and API mapping listing, the nil-page guard, and the
forbidden-method interface contract.

No-Regression Evidence: `go test ./internal/collector/awscloud/...` covers the
adapter together with the scanner and runtime registration.

Collector Observability Evidence: the adapter records the
`aws.service.pagination.page` span and `eshu_dp_aws_api_calls_total` and
`eshu_dp_aws_throttle_total` counters per operation.

No-Observability-Change: the adapter reuses the shared AWS collector telemetry
contract and adds no new metric names.

## Related docs

- `../README.md`
- `docs/public/services/collector-aws-cloud-scanners.md`
- `docs/public/guides/collector-authoring.md`
