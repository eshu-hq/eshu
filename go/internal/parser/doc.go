// Package parser owns the native Go parser registry, language adapters, and
// SCIP reduction support used to extract source-level entities and metadata.
//
// The package exposes a registry of language parsers, source-level entity
// and relationship extraction helpers, import alias, resolved-source, and
// constructor receiver metadata, dead-code root metadata for functions, types,
// and package entrypoint files, package-level interface/type reference pre-scans,
// nearest-package JavaScript roots, CommonJS module.exports alias and mixin
// roots, JSONC tsconfig comment/trailing-comma baseUrl and paths metadata,
// Hapi handler, plugin, and route-reference roots, Next.js app and route
// exports, Express/Koa/Fastify/NestJS callback roots, Node migration exports,
// TypeScript interface implementation, module-contract, package
// public-surface, and exported static-registry roots, returned function-value
// references, static re-export metadata, composite-literal type references,
// Helm/YAML metadata extraction, and SCIP support for index-derived facts.
// Parser changes must preserve fact truth: when a parser starts emitting a new
// entity, relationship, or metadata field, the relevant fixtures, fact
// contracts in internal/facts, and downstream docs must move in lockstep.
// Parsers must be deterministic given the same source bytes so retries and
// repair runs converge.
package parser
