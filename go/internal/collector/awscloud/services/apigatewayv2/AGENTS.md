# API Gateway v2 Service Package

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `types.go`
4. `scanner.go`
5. `relationships.go`
6. `awssdk/README.md`

Keep the scanner metadata-only. Do not add the OpenAPI export, integration or
route response reads, model or template reads, request/response mapping-template
persistence, route request-model persistence, authorizer invocation URI or
credential ARN persistence, JWT secret persistence, stage variable value
persistence, or any mutation API.

The scanner boundary must remain `awscloud.ServiceAPIGatewayV2`. This service
kind covers only HTTP and WebSocket APIs; the classic REST (v1) surface stays in
the `apigateway` scanner.

Every relationship must set a non-empty `target_type` and join by the
resource_id the target scanner publishes: Lambda by function ARN, Cognito user
pool by the bare pool id parsed from the JWT issuer URL, ACM by certificate ARN,
EC2 subnet/security group by bare id, VPC link by link id. Do not target the
full JWT issuer URL for a Cognito edge or the edge will dangle.

Do not set `RequiresRedactionKey` in the runtimebind registration. The scanner
drops templates and secrets by never mapping them, so it needs no redaction key.
