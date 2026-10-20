# API Gateway v2 AWS SDK Adapter

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `client.go`
4. `list.go`
5. `mapper.go`
6. `helpers.go`
7. `../README.md`

Allowed API calls are `GetApis`, `GetStages`, `GetRoutes`, `GetIntegrations`,
`GetAuthorizers`, `GetVpcLinks`, `GetDomainNames`, and `GetApiMappings`.

Do not add `ExportApi`, `GetIntegrationResponse(s)`, `GetRouteResponse(s)`,
`GetModel(s)`, `GetModelTemplate`, `ReimportApi`/`ImportApi`, or any mutation
call without a new issue and evidence note. The forbidden-method reflection test
fails the build if a forbidden method enters the `apiClient` interface.

Never map `Route.RequestModels`, `Route.RequestParameters`,
`Integration.RequestTemplates`, `Integration.RequestParameters`,
`Integration.ResponseParameters`, `Integration.CredentialsArn`,
`Authorizer.AuthorizerUri`, `Authorizer.AuthorizerCredentialsArn`, or
`Authorizer.IdentityValidationExpression`. New AWS calls must stay paginated,
wrapped in `recordAPICall`, and covered by fake-client tests that prove the
request shape and safe mapping.
