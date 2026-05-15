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
// are stable wire contracts. The dead-code OpenAPI fragment names modeled
// language roots and keeps the language filter examples aligned with C#, C,
// Dart, Haskell, Kotlin, Elixir, Perl, PHP, Groovy, and SQL query behavior. That
// filter is part of the dogfood contract for validating one language family
// without earlier candidate labels filling the page. Dead-code responses preserve language maturity, modeled
// framework-root lists, and root-kind evidence for functions and types so
// callers can separate cleanup candidates from modeled roots; TypeScript
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
// where-clause evidence, plus C c.main_function, c.public_header_api,
// c.signal_handler, c.callback_argument_target, and c.function_pointer_target
// roots, plus C# main, constructor, override, interface, ASP.NET controller,
// hosted-service, test, and serialization roots, plus C++
// cpp.main_function, cpp.public_header_api, cpp.virtual_method,
// cpp.override_method, cpp.callback_argument_target, and
// cpp.function_pointer_target roots, plus cpp.node_addon_entrypoint, plus Kotlin
// top-level main, constructor, interface, override, Gradle, Spring, lifecycle,
// and JUnit roots, plus Scala main, App object, trait, override, Play, Akka,
// lifecycle, JUnit, and ScalaTest roots, plus Swift main, SwiftUI, protocol,
// constructor, override, UIKit application delegate, Vapor, XCTest, and Swift
// Testing roots, plus Elixir Application start, public macro, public guard,
// behaviour callback, GenServer, Supervisor, Mix task, protocol, Phoenix
// controller, and LiveView roots, plus Dart main, constructor, override,
// Flutter build/createState, and public library API roots, plus Ruby
// Rails controller/callback roots, dynamic-dispatch hooks, literal
// method-reference targets, and script entrypoints, plus Groovy Jenkinsfile
// pipeline entrypoints and vars/*.groovy shared-library call roots, plus
// Haskell main, explicit module-export, exported type, typeclass method, and
// instance method roots, plus Perl script entrypoints, public package
// namespaces, Exporter exports, constructors, special blocks, AUTOLOAD, and
// DESTROY roots, plus PHP script entrypoints, constructors, known magic methods, same-file
// interface/trait methods, route-backed controller actions, route handlers,
// Symfony route attributes, and WordPress hook callbacks. C, C++, Perl, PHP,
// Ruby, Haskell, and Rust now share the derived dead-code maturity tier with Go and Java while
// exact cleanup remains gated on broader semantic resolution.
// C#, Kotlin, Scala, and Elixir share that tier through parser-backed roots for
// common framework and language entrypoints; Elixir Application, OTP, Phoenix
// controller, and LiveView roots use syntax and arity checks before
// suppression. Groovy remains candidate-only until dynamic dispatch, closure
// delegates, shared library loading, and pipeline DSL steps have stronger
// semantic resolution. Rust Cargo auxiliary target files under benches/ and
// examples/ are treated like non-production roots for cleanup analysis. Rust exactness
// blockers are reported in the analysis payload for
// unresolved macro expansion, cfg and Cargo feature selection, semantic module
// resolution, and trait dispatch, with observed blocker reporting for returned
// candidates that carry parser metadata. C exactness blockers for macro
// expansion, conditional compilation, build target selection, include graphs,
// callback registration, dynamic symbol lookup, and external linkage are
// reported the same way. C++ exactness blockers add template instantiation,
// overload resolution, and virtual dispatch breadth to those C-style blockers,
// C# blockers cover reflection, dependency injection, source generators,
// partial types, dynamic dispatch, project references, and public API surfaces,
// Kotlin blockers cover reflection, dependency injection, annotation
// processing, compiler plugins, dynamic dispatch, Gradle source sets,
// multiplatform targets, and public API surfaces,
// Scala blockers cover macro expansion, implicit/given resolution, dynamic
// dispatch, reflection, sbt source sets, framework route files, compiler plugin
// output, and public API surfaces,
// Swift blockers cover macro expansion, conditional compilation, SwiftPM target
// resolution, protocol witnesses, dynamic dispatch, generated property-wrapper
// and result-builder code, Objective-C runtime dispatch, and public API surfaces,
// Elixir blockers cover macro expansion, dynamic dispatch, behaviour callback
// resolution, protocol dispatch, Phoenix route resolution, supervision trees,
// Mix environment selection, and public API surfaces,
// Dart blockers cover part-file library resolution, conditional import/export
// selection, package export surfaces, dynamic dispatch, Flutter route/lifecycle
// wiring, generated code, mirrors, and public API surfaces,
// PHP blockers cover dynamic dispatch, reflection, Composer autoloading,
// include/require resolution, framework routing, trait resolution, namespace
// aliases, magic-method dispatch, and public API surfaces,
// Perl blockers cover symbolic reference dispatch, AUTOLOAD dispatch,
// inheritance resolution, Moose/Moo metadata, import side effects, runtime
// eval, and public API surfaces,
// Ruby exactness blockers cover metaprogramming, autoload, framework routing,
// gem public API, and constant resolution, Groovy exactness blockers cover
// dynamic dispatch, closure delegates, Jenkins shared libraries, and pipeline
// DSL dynamic steps, Haskell blockers cover Template Haskell, CPP conditional
// compilation, Cabal component membership, implicit module exports, typeclass
// dispatch, module re-exports, and FFI callbacks, and candidates with observed blockers classify as
// ambiguous instead of cleanup-ready unused. SQL SqlFunction
// routines are scanned as derived candidates, SQL dynamic/routine/migration
// blockers are reported, and batched exact graph incoming probes let
// reducer-written SQL EXECUTES edges protect trigger-bound routines. The
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
// label-scoped and repo-anchored when a repository is supplied, allows bounded
// deterministic content-model candidate paging when repo_id is omitted,
// de-duplicates entity IDs across scanned candidate labels, prefers
// content-model candidate paging before graph fallback, pushes any language
// filter into the candidate query, applies content-backed policy checks before
// repo-grouped relational code-call and inheritance incoming-edge lookups,
// hydrates candidate metadata through batch GetEntityContents reads, supports a
// language filter so one language family can be validated without earlier
// candidate labels filling the page, keeps batched exact graph probes as a
// fallback for SQL routine reachability, keeps a bounded scan window for small
// result limits, and reports display truncation separately from bounded raw
// candidate pages and rows so callers can tell whether the result list was
// clipped or the candidate scan cap was reached. HCL is reported in the
// dead-code maturity metadata as `non_code_iac_evidence`, and explicit HCL
// dead-code requests do not scan source-code candidate labels. C root
// suppressions are honored from content-store metadata after hydration, and
// C#, C++, Kotlin, Scala, Elixir, Haskell, Perl, PHP, Ruby, and Groovy root
// suppressions use the same graph/content metadata path.
// That matches the
// normal parser metadata path used by indexed repositories.
// Infrastructure reads expose Terraform backend, import, moved, removed, check,
// and lockfile-provider evidence as first-class entity types once parser and
// projector support exists.
// Legacy impact and environment-comparison reads expose explicit list limits
// and truncation metadata so prompt-facing MCP calls do not depend on cache
// warmth or silently incomplete graph scans.
// Evidence citation packets hydrate bounded file and entity handles from the
// content store without graph traversal, reject input arrays above 500 handles,
// and hydrate at most 50 citations per call so story, investigation, and
// documentation prompts can cite source, docs, manifests, and deployment proof.
// Repository runtime artifacts surface Dockerfile base image, base tag, build
// platform, copy-from, command, port, and environment evidence from parser
// metadata. Deployment trace image references can include projected OCI registry
// truth when digest-addressed ContainerImage-family rows, or tag observations
// with one projected immutable digest, are available; tag-only conflicts remain
// ambiguous and are counted separately from canonical digest matches. The OCI
// read path starts from digest or image_ref anchors before traversing registry
// publish edges.
// Content-backed Argo CD relationship fallback treats Application
// source_repos as separate DEPLOYS_FROM targets while preserving source_repo
// for older parser payloads.
// local_authoritative and local_full_stack both answer graph-backed platform
// impact queries, while local_lightweight returns structured unsupported errors
// for those routes. Change-surface investigation resolves ambiguous targets
// before graph traversal, accepts bare service names through canonical
// workload-id probes, keeps repo-scoped workload probes constrained by name,
// keeps generic target resolution on bounded known-label probes, anchors
// resolved traversal by typed target labels, and returns code-topic or
// changed-path handles with bounded direct/transitive impact rows.
// Repository coverage reads content-store counts first and reports graph parity
// only when the graph coverage fallback actually ran, so large
// repositories can answer coverage without an unbounded graph count.
package query
