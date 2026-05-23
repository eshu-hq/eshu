# AGENTS.md - services/apigateway/awssdk

Read `README.md`, `doc.go`, `client.go`, `rest.go`, `v2.go`, `mapper.go`,
`helpers.go`, and `../README.md` before editing this adapter.

## Mandatory Rules

- Keep AWS SDK calls in this adapter and map responses into scanner-owned
  types.
- Allowed calls are REST `GetRestApis`, `GetStages`, `GetResources`,
  `GetDomainNames`, `GetBasePathMappings`, and v2 `GetApis`, `GetStages`,
  `GetIntegrations`, `GetDomainNames`, `GetApiMappings`.
- Wrap new pages and point reads in `recordAPICall`; keep pagination bounded.
- Do not add execution, export, API key, authorizer, policy body, credential,
  payload, template body, or mutation calls without tests and docs proving a
  metadata-only need.
- Keep operation labels aligned with AWS SDK names and keep identifiers,
  payloads, page tokens, raw AWS errors, and secrets out of metric labels.
