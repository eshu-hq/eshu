// Package golang owns Go source parsing behind the parent parser dispatch
// boundary.
//
// Parse builds the Go payload consumed by collector materialization, including
// functions, methods, structs, interfaces, imports, variables, function calls,
// method return-type metadata, dead-code root evidence, composite-literal type
// references, and embedded SQL rows. Receiver inference covers typed
// parameters, constructor-assigned locals, and map-value range variables while
// function-value references cover call arguments, composite literal fields, and
// returned method values. Those roots remain bounded by local lexical bindings
// so parser metadata does not claim reachability from shadowed variables.
// PreScan and ImportedInterfaceParamMethods provide the lighter package
// evidence that the parent Engine uses before full parsing. Helper functions
// preserve the parent parser's branch-counting contract for cyclomatic
// complexity, including range loops. The package uses shared helper contracts
// instead of parent parser helpers, so language-owned adapters do not create
// dispatcher import cycles.
package golang
