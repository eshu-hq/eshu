// Package query owns the HTTP read surfaces, OpenAPI assembly, and read
// models that back the public Eshu query API.
//
// Handlers depend on graph and content ports such as GraphQuery and the
// Postgres content reader rather than concrete backends, so backend
// dialect differences stay in narrow seams. The public OpenAPI contract is
// built from openapi*.go fragments and served at /api/v0/openapi.json;
// handler behavior, OpenAPI fragments, and docs/docs/reference/http-api.md
// must agree whenever public routes or response shapes change. Response
// envelopes, truth metadata, capability gates, and code-quality classifications
// are stable wire contracts. Dead-code responses preserve language maturity,
// modeled framework-root lists, and root-kind evidence for functions and types
// so callers can separate cleanup candidates from modeled roots; TypeScript
// interface implementation, module-contract, and static-registry roots are
// reported alongside JavaScript-family package, CommonJS mixin, Next.js,
// Express, Koa, Fastify, NestJS, migration, and framework roots. Unsupported
// language metadata and test fixtures are suppressed from default cleanup
// candidates. The dead-code scan applies cheap graph-side path
// filters before content-backed policy checks, keeps a 10,000-row window for
// small result limits, and reports display truncation separately from bounded
// raw candidate pages and rows so callers can tell whether the result list was
// clipped or the graph scan cap was reached.
// local_authoritative and local_full_stack both answer graph-backed platform
// impact queries, while local_lightweight returns structured unsupported errors
// for those routes.
package query
