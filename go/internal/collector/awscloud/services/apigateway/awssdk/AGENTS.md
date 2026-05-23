# API Gateway AWS SDK Adapter

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `client.go`
4. `rest.go`
5. `v2.go`
6. `mapper.go`
7. `helpers.go`
8. `../README.md`

Allowed API calls are REST `GetRestApis`, `GetStages`, `GetResources`,
`GetDomainNames`, `GetBasePathMappings`, and v2 `GetApis`, `GetStages`,
`GetIntegrations`, `GetDomainNames`, `GetApiMappings`.

Do not add API execution, export, API key, authorizer, policy body,
credential, payload, template body, or mutation calls without a new issue and
evidence note. New AWS calls must stay paginated, wrapped in `recordAPICall`,
and covered by fake-client tests that prove the request shape and safe mapping.
