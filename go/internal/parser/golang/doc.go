// Package golang owns Go source parsing behind the parent parser dispatch
// boundary.
//
// Parse builds the Go payload consumed by collector materialization, including
// functions, methods, structs, interfaces, imports, variables, function calls,
// method return-type metadata, dead-code root evidence, composite-literal type
// references, embedded SQL rows, and command-execution call-site rows. Function
// and method rows carry
// package_import_path and stable scip_symbol values only when callers provide
// GoPackageImportPath; package-qualified imported calls carry matching
// stable_symbol_key values only when the import path is known. Return-type
// metadata normalizes pointers, slices, arrays, selector types, and generic
// instantiations to the terminal element type. Import rows preserve explicit
// aliases, and chained method calls carry receiver proof only when local
// lexical evidence identifies the root receiver type. Receiver inference covers
// typed parameters on declarations and function literals,
// constructor-assigned locals, and
// map-value range variables while function-value references cover call
// arguments, composite literal fields, returned method values, callback
// literals, composite-literal registries, direct method calls, generic
// constraint methods, fmt Stringer methods, and concrete values that escape
// through same-repo imported package interface parameters, and methods called
// through imported package receiver types. Method-return chain metadata
// requires concrete receiver proof, so interface-only parameters and ambiguous
// local assignments stay unresolved. Direct method roots require scoped
// receiver evidence or a bounded struct-field receiver type; unknown receivers
// are not rooted by same-method-name fallback. Generic receiver class context
// is normalized to the base receiver type before payload emission. Those roots
// remain bounded by local lexical bindings and qualified package contracts so
// parser metadata does not claim reachability from shadowed variables, unused
// local closures, unrelated same-named imported functions, or fmt writer and
// format-string arguments. PreScan,
// ImportedInterfaceParamMethods, ExportedInterfaceParamMethods, and
// ImportedDirectMethodCallRoots provide the lighter package evidence that the
// parent Engine uses before full parsing; same-repo exported interface
// contracts carry package interface method names instead of unbounded exported
// method roots. Package-level generic constraints and local interface return
// signatures are combined by the parent parser before full parsing. Imported
// direct-method pre-scans only query scoped imported-variable types for selector
// calls, since bare function calls cannot produce imported receiver roots. The
// imported-variable type index scans package-scope declarations without walking
// function bodies during its top-level pass, then lazily replays scoped local
// bindings only for selector calls that need receiver proof. Helper functions
// preserve the parent parser's branch-counting
// contract for cyclomatic complexity, including range loops. The package uses
// shared helper contracts instead of parent parser helpers, so language-owned
// adapters do not create dispatcher import cycles.
//
// When Options.EmitDataflow is set, Parse also emits a "dataflow_functions"
// bucket (per-function control-flow graphs and reaching-definition def->use
// edges built by cfg_lower.go over the internal/parser/cfg engine; selector
// reads and writes keep field-sensitive access paths, and straight-line pointer
// aliases to local structs are normalized before the edge is emitted; deep
// access paths are capped and counted in the row overflow) and a
// "taint_findings" bucket (intraprocedural source-to-sink taint findings with
// confidence and provenance, built by cfg_taint_facts.go over the
// internal/parser/taint engine), an "interproc_findings" bucket
// (cross-function taint findings within the file, built by cfg_effects.go and
// cfg_interproc.go: each function's value-flow summary is derived over
// internal/parser/valueflow and composed into an interprocedural port graph
// solved by internal/parser/interproc), and, when RepositoryID and
// GoPackageImportPath are present, a "dataflow_summaries" bucket of durable
// summary.Effects rows for reducer persistence. The gate is off by default and
// the payload is byte-identical when off, so existing fact contracts are
// untouched unless a caller opts in. Embedded shell-command evidence records
// only structural os/exec call metadata; command text, arguments, and
// environment values are intentionally omitted.
package golang
