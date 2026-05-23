# AGENTS.md - services/apigateway

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep API Gateway AWS access behind `Client`; the scanner package must not
  import the AWS SDK.
- Keep the boundary `awscloud.ServiceAPIGateway`; REST, HTTP, and WebSocket API
  Gateway metadata share this service kind.
- Emit reported metadata evidence only. Do not infer workload, environment,
  repository, ownership, or deployable-unit truth.
- Do not persist API keys, authorizer secrets, policy JSON, integration
  credentials, stage variable values, mapping templates, request or response
  templates, or payloads.
- Add relationship evidence only when API Gateway directly reports both sides.
- Keep API names, ARNs, tags, domains, log destinations, raw AWS errors, and
  integration URIs out of metric labels.
