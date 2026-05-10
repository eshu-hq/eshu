// Package golang owns Go source parsing behind the parent parser dispatch
// boundary.
//
// Parse builds the Go payload consumed by collector materialization, including
// functions, methods, structs, interfaces, imports, variables, function calls,
// method return-type metadata, dead-code root evidence, composite-literal type
// references, and embedded SQL rows. Import rows preserve explicit aliases, and
// chained method calls carry receiver proof only when local lexical evidence
// identifies the root receiver type. Receiver inference covers typed parameters
// on declarations and function literals, constructor-assigned locals, and
// map-value range variables while function-value references cover call
// arguments, composite literal fields, returned method values, callback
// literals, and composite-literal registries. Those roots remain bounded by
// local lexical bindings so parser metadata does not claim reachability from
// shadowed variables or unused local closures.
// PreScan and ImportedInterfaceParamMethods provide the lighter package
// evidence that the parent Engine uses before full parsing. Helper functions
// preserve the parent parser's branch-counting contract for cyclomatic
// complexity, including range loops. The package uses shared helper contracts
// instead of parent parser helpers, so language-owned adapters do not create
// dispatcher import cycles.
package golang
