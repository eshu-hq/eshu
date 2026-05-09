// Package parser owns the native Go parser registry, language adapters, and
// SCIP reduction support used to extract source-level entities and metadata.
//
// The package exposes a registry of language parsers, source-level entity
// and relationship extraction helpers, import alias, resolved-source, and
// constructor receiver metadata, dead-code root metadata for functions, types,
// and package entrypoint files, package-level interface/type reference pre-scans,
// nearest-package JavaScript roots, Python route/task/CLI and AWS Lambda root
// metadata, Python method class context, constructor calls, class receiver
// references, dataclass/property roots, dunder protocol roots, inheritance
// base names, bounded __all__/__init__.py public API roots, bases, and members,
// and local constructor or self receiver metadata without marking every
// non-underscore Python symbol live, Jupyter notebook source extraction, CommonJS
// module.exports alias and mixin roots, JSONC tsconfig comment/trailing-comma
// baseUrl and paths
// metadata, Hapi handler, plugin, and exported route-array reference roots
// including direct, config, and options handlers, Next.js app and route
// exports, Express/Koa/Fastify/NestJS callback roots, Node migration exports,
// TypeScript interface implementation, module-contract, package public-surface,
// and exported static-registry roots, Java main, constructor, override,
// JavaBean-style public Ant Task setter roots, Gradle plugin apply roots,
// Gradle task action/property roots, and public Gradle DSL method roots,
// Java method-reference metadata, local receiver type metadata backed by a
// per-file variable and field index, and arity metadata for overload-safe call
// resolution, returned function-value
// references, static re-export metadata, composite-literal type references,
// Helm/YAML metadata extraction, and SCIP support for index-derived facts.
// Parser changes must preserve fact truth: when a parser starts emitting a new
// entity, relationship, or metadata field, the relevant fixtures, fact
// contracts in internal/facts, and downstream docs must move in lockstep.
// Parsers must be deterministic given the same source bytes so retries and
// repair runs converge.
package parser
