// Package appmesh maps AWS App Mesh metadata into AWS cloud collector facts.
//
// The package owns scanner-level App Mesh normalization only. It never calls
// the AWS SDK directly and never calls an App Mesh mutation API. SDK adapters
// provide already-resolved Mesh values (with virtual services, virtual nodes,
// virtual routers, routes, virtual gateways, and gateway routes), and Scanner
// emits aws_resource facts plus aws_relationship facts for the edges App Mesh
// reports directly:
//
//   - virtual service -> mesh
//   - virtual node -> backend virtual service
//   - route -> virtual router
//   - virtual gateway -> mesh
//   - virtual node -> ACM certificate authority (client TLS validation trust)
//   - virtual node -> Cloud Map service or DNS hostname (service discovery)
//
// Two payload classes are never persisted. The scanner does not store a client
// TLS validation certificate body; client TLS validation is reduced to ACM
// certificate authority ARN references only. Sensitive HTTP header match values
// (Authorization, Cookie, X-Api-Key shaped) are redacted through the shared
// redact library before emission, while the header name and route shape (path
// prefix, method, match type) are always preserved. Scanner requires a
// redaction key and fails closed when it is zero.
package appmesh
