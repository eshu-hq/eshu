// Package apigateway emits metadata-only AWS API Gateway facts for REST, HTTP,
// and WebSocket APIs.
//
// The package owns scanner-side resource and relationship fact shaping for API
// identities, stages, custom domains, domain mappings, access-log
// destinations, certificate dependencies, and ARN-addressable integration
// targets. It deliberately excludes API key material, authorizer secrets,
// policy JSON, integration credentials, stage variable values, request and
// response templates, and runtime payloads. Reducers, not this package, own any
// workload, repository, environment, or deployable-unit inference.
package apigateway
