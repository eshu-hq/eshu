// Package query owns the HTTP read surfaces, OpenAPI assembly, and read
// models that back the public Eshu query API.
//
// Handlers depend on graph and content ports such as GraphQuery and the
// Postgres content reader rather than concrete backends, so backend
// dialect differences stay in narrow seams. The public OpenAPI contract is
// built from openapi*.go fragments and served at /api/v0/openapi.json;
// handler behavior, OpenAPI fragments, and docs/docs/reference/http-api.md
// must agree whenever public routes or response shapes change. Response
// envelopes and truth metadata are stable wire contracts and must not
// change in ways that break MCP tool dispatch.
package query
