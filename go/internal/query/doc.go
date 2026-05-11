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
// public API export, public API re-export, public type-reference, interface
// implementation, module-contract, and static-registry roots are reported
// alongside JavaScript-family package, CommonJS mixin, Next.js, Express, Koa,
// Fastify, NestJS, migration, and framework roots, plus Python
// route, worker, CLI, AWS Lambda handler, dataclass, post-init, property,
// dunder protocol, __all__, package __init__.py, public API base, and public
// API member roots, plus Java main, constructor, override, Ant Task setter,
// Gradle plugin apply, task action/property, task setter, task-interface method,
// public Gradle DSL, same-class method-reference target, Spring component and
// callback, lifecycle, JUnit, Jenkins, Stapler, serialization hook, bounded
// reflection, ServiceLoader, and Spring auto-configuration roots, plus Rust
// Cargo entrypoint, build-script, unit-test, Tokio runtime/test, public API,
// benchmark, trait-implementation method, path-attribute module, direct module
// resolution, macro-declaration, conditional-derive, nested-annotation, and
// where-clause evidence. Rust now shares the derived dead-code maturity tier
// with Go and Java while exact Rust cleanup remains gated on broader semantic
// resolution. Rust Cargo auxiliary target files under benches/ and examples/
// are treated like non-production roots for cleanup analysis. Rust exactness
// blockers are reported in the analysis payload for
// unresolved macro expansion, cfg and Cargo feature selection, semantic module
// resolution, and trait dispatch, with observed blocker reporting for returned
// candidates that carry parser metadata, and candidates with observed blockers
// classify as ambiguous instead of cleanup-ready unused. The
// analysis notes and modeled-root list use the same Java root family so callers
// see why those entities were suppressed. The analysis payload names modeled
// root kinds, includes Go same-package and imported-package direct method
// calls, generic constraint methods, fmt Stringer methods, plus
// function-literal reachable calls in the modeled Go root list, reports
// reflection support, and counts parser-metadata suppressions so callers can
// explain why an entity was not returned as a cleanup candidate.
// The modeled-root list names the Rust root kinds the policy suppresses.
// Unsupported language metadata and test fixtures are suppressed from default
// cleanup candidates. The dead-code scan keeps raw candidate reads
// label-scoped and repo-anchored, prefers
// content-model candidate paging before graph fallback, applies content-backed
// policy checks before relational code-call and inheritance incoming-edge
// lookups, hydrates candidate metadata through batch GetEntityContents reads,
// keeps exact graph probes as a fallback, keeps a bounded scan window for small
// result limits, and reports display truncation separately from bounded raw
// candidate pages and rows so callers can tell whether the result list was
// clipped or the graph scan cap was reached.
// Infrastructure reads expose Terraform backend, import, moved, removed, check,
// and lockfile-provider evidence as first-class entity types once parser and
// projector support exists.
// local_authoritative and local_full_stack both answer graph-backed platform
// impact queries, while local_lightweight returns structured unsupported errors
// for those routes.
package query
