// Package parser owns the native Go parser registry, language adapters, and
// SCIP reduction support used to extract source-level entities and metadata.
//
// The package exposes a registry of language parsers, source-level entity
// and relationship extraction helpers, import alias, resolved-source, and
// constructor receiver metadata, dead-code root metadata for functions, types,
// and package entrypoint files, package-level interface/type reference pre-scans,
// Go package semantic root pre-scans through PreScanGoPackageSemanticRoots,
// nearest-package JavaScript roots extracted through the JavaScript helper
// subpackage, Python route/task/CLI and AWS Lambda root metadata, Python method
// class context, constructor calls, class receiver
// references, dataclass/property roots including cached properties, class
// protocol methods, module dunder hooks, assignment-proven nested dunder
// protocol roots, inheritance base names, bounded __all__/__init__.py public
// API roots, bases, and members, and local
// constructor or self receiver metadata without marking every non-underscore
// Python symbol live, Jupyter notebook source extraction through
// the Python helper subpackage, CommonJS module.exports alias and mixin roots,
// JSONC tsconfig comment/trailing-comma
// baseUrl and paths metadata extracted through the JavaScript helper
// subpackage, Hapi handler, plugin, and exported route-array reference roots
// including direct, config, and options handlers, Next.js app and route
// exports, Express/Koa/Fastify/NestJS callback roots, Node migration exports,
// TypeScript interface implementation, module-contract, package public-surface,
// and exported static-registry roots, Java main, constructor, override,
// JavaBean-style public Ant Task setter roots, Gradle plugin apply roots,
// Gradle task action/property roots, Gradle task setter and task-interface
// roots, public Gradle DSL method roots, and same-class method-reference target
// roots. C roots include main functions, directly included public-header
// declarations, signal handlers, callback argument targets, and direct
// function-pointer initializer targets. C++ roots include main functions,
// directly included public-header declarations, virtual and override methods,
// callback argument targets, direct function-pointer initializer targets, and
// Node native-addon entrypoints. Java roots also include Spring
// component/configuration-property
// classes, Spring request/bean/event/scheduled methods, Java lifecycle
// callbacks, JUnit test and lifecycle methods, Jenkins extension/symbol/
// initializer/data-bound setter methods, Stapler web methods, and serialization
// hook methods. Java metadata payload assembly stays in the parent wrapper,
// while the Java helper subpackage extracts method-reference rows, bounded literal
// reflection references, ServiceLoader provider and Spring auto-configuration
// metadata files extracted through the Java helper subpackage, local receiver
// type metadata backed by a per-file variable, enhanced-for variable, field,
// and typed-lambda index, plus record class context, unqualified same-class and
// enclosing-class call context, explicit outer-this field receivers, typed
// method-reference receiver metadata, arity metadata, same-class method return
// types for argument metadata, and type-signature metadata for overload-safe
// call resolution.
// Java pre-scan includes records in the same source-name map as classes and
// methods. The package also emits Go embedded SQL metadata through the Go
// helper subpackage, Groovy/Jenkins delivery metadata through the Groovy helper
// subpackage, Dockerfile runtime metadata through the Dockerfile helper
// subpackage, JSON and JSONC document extraction through the JSON helper
// subpackage,
// YAML document extraction through the YAML helper subpackage,
// CloudFormation/SAM template extraction through the CloudFormation helper
// subpackage, returned function-value references, static re-export metadata,
// Go direct method-call roots, Go imported-package direct method-call roots,
// Go same-repo imported package interface roots, composite-literal type
// references, package-level generic constraint roots, package-level chained
// receiver roots from local interface methods that return imported types, and
// SCIP support for index-derived facts.
// dbt SQL lineage extraction through the dbt SQL helper subpackage. Language
// child packages also own Go, Java, Python, JavaScript/TypeScript/TSX, C, C++,
// Rust, C#, Scala, Elixir, Swift, Dart, Ruby, Perl, Haskell, SQL, and
// HCL/Terraform parse and pre-scan behavior behind thin parent wrappers. The
// Rust wrapper additionally annotates direct module declarations with bounded
// repo-relative resolution status when ParsePath has a repo root, and it does
// not promote outside-repo files to resolved module evidence. The parent
// bridge converts Options and payload helpers into shared parser helper
// contracts instead of making child packages import the parent dispatcher.
// Parser changes must preserve fact truth: when a parser emits a new
// entity, relationship, or metadata field, the relevant fixtures, fact
// contracts in internal/facts, and downstream docs must move in lockstep.
// Parsers must be deterministic given the same source bytes so retries and
// repair runs converge.
package parser
