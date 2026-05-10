// Package golang owns Go source parsing behind the parent parser dispatch
// boundary.
//
// Parse builds the Go payload consumed by collector materialization, including
// functions, methods, structs, interfaces, imports, variables, function calls,
// method return-type metadata, dead-code root evidence, composite-literal type
// references, and embedded SQL rows. Return-type metadata normalizes pointers,
// slices, arrays, selector types, and generic instantiations to the terminal
// element type. Import rows preserve explicit aliases, and chained method calls
// carry receiver proof only when local lexical evidence identifies the root
// receiver type. Receiver inference covers typed parameters on declarations
// and function literals, constructor-assigned locals, and
// map-value range variables while function-value references cover call
// arguments, composite literal fields, returned method values, callback
// literals, composite-literal registries, direct method calls, generic
// constraint methods, fmt Stringer methods, and concrete values that escape
// through same-repo imported package interface parameters, and methods called
// through imported package receiver types. Direct method roots require scoped
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
// signatures are combined by the parent parser before full parsing. Helper
// functions preserve the parent parser's branch-counting
// contract for cyclomatic complexity, including range loops. The package uses
// shared helper contracts instead of parent parser helpers, so language-owned
// adapters do not create dispatcher import cycles.
package golang
