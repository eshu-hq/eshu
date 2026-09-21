# API Gateway Service Package

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `types.go`
4. `scanner.go`
5. `relationships.go`
6. `awssdk/README.md`

Keep the scanner metadata-only. Do not add API execution calls, export calls,
API key reads, authorizer secret reads, policy JSON persistence, integration
credential persistence, request or response template persistence, payload
reads, or mutation APIs.

The scanner boundary must remain `awscloud.ServiceAPIGateway`. REST APIs and v2
HTTP/WebSocket APIs share this service kind so one claim can describe the full
regional API Gateway edge surface without collector-owned workload inference.
