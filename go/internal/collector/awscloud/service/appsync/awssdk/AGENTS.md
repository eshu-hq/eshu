# AppSync AWS SDK Adapter

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `client.go`
4. `list.go`
5. `mapper.go`
6. `../README.md`

Allowed API calls are `ListGraphqlApis`, `ListDataSources`, `ListTypes`,
`ListResolvers`, `ListFunctions`, `ListApiKeys`, and `GetSchemaCreationStatus`.

Do not add EvaluateMappingTemplate, EvaluateCode, GetIntrospectionSchema,
StartSchemaCreation, GetDataSourceIntrospection, any Get* that returns a
template, code, or schema body, or any mutation call without a new issue and an
evidence note. The forbidden-method reflection test must keep passing.

Never read `Type.Definition` (SDL body). Never map `RequestMappingTemplate`,
`ResponseMappingTemplate`, `Code`, the HTTP `AuthorizationConfig`, the RDS
`AwsSecretStoreArn`, or any API key value. The scanner-owned types have no field
for them; keep it that way.

New AWS calls must stay paginated with a correct `NextToken` loop and nil-page
guard, wrapped in `recordAPICall`, and covered by fake-client tests that prove
the request shape and safe mapping.
