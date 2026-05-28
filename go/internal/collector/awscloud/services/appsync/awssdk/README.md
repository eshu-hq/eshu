# AppSync AWS SDK Adapter

## Purpose

`internal/collector/awscloud/services/appsync/awssdk` adapts AWS SDK for Go v2
AppSync control-plane responses into the scanner-owned metadata model defined by
the parent `appsync` package. It pages read-only list operations and maps SDK
shapes into safe metadata records.

## Ownership boundary

This package owns AppSync SDK pagination, response mapping, and per-call
telemetry. It does not own fact envelope shaping, relationship logic, or
registration. Fact shaping lives in the parent `appsync` package; registration
lives in the sibling `runtimebind` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the AppSync SDK adapter implementing `appsync.Client`.
- `NewClient` - builds an adapter for one claimed AWS boundary.

The accepted SDK surface is the unexported `appsyncAPI` interface. A reflection
test asserts it cannot reach mapping-template evaluation, code evaluation,
schema-body reads, schema-creation, introspection, or mutation operations.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/appsync` and its `types` package.
- `internal/collector/awscloud` for the boundary and shared API-call telemetry.
- `internal/collector/awscloud/services/appsync` for the scanner-owned types.
- `internal/telemetry` for spans and instruments.

## Telemetry

Each AWS operation runs inside `recordAPICall`, which starts the
`aws.service.pagination.page` span and records `eshu_dp_aws_api_calls_total` and
`eshu_dp_aws_throttle_total` with the bounded AWS collector labels.

## Gotchas / invariants

- Allowed API calls are `ListGraphqlApis`, `ListDataSources`, `ListTypes`,
  `ListResolvers`, `ListFunctions`, `ListApiKeys`, and
  `GetSchemaCreationStatus`. Do not add EvaluateMappingTemplate, EvaluateCode,
  GetIntrospectionSchema, StartSchemaCreation, GetDataSourceIntrospection, any
  Get* that returns a body, or any mutation call without a new issue and an
  evidence note.
- `ListResolvers` requires a type name, so the adapter lists type names once
  (without reading any `Type.Definition` SDL body) and reuses them for both
  resolver enumeration and the schema type count. Never read `Type.Definition`.
- The mappers never copy `Resolver.RequestMappingTemplate`,
  `Resolver.ResponseMappingTemplate`, `Resolver.Code`,
  `FunctionConfiguration.RequestMappingTemplate`,
  `FunctionConfiguration.ResponseMappingTemplate`, `FunctionConfiguration.Code`,
  the HTTP `AuthorizationConfig`, the RDS `AwsSecretStoreArn`, or any API key
  value. The scanner-owned types have no field for them.
- Pagination follows `NextToken` and stops on nil or empty; treating empty as
  continuation would loop forever. Nil pages are guarded.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/appsync/awssdk/...`
covers paginated GraphQL API discovery, per-API data source, type-name, resolver,
function, schema-status, and API key listing, the nil-page guard, and the
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
