// Package golang owns Go source parsing behind the parent parser dispatch
// boundary.
//
// Parse builds the Go payload consumed by collector materialization, including
// functions, methods, structs, interfaces, imports, variables, function calls,
// dead-code root evidence, composite-literal type references, and embedded SQL
// rows. PreScan and ImportedInterfaceParamMethods provide the lighter package
// evidence that the parent Engine uses before full parsing. Helper functions
// preserve the parent parser's branch-counting contract for cyclomatic
// complexity, including range loops. The package uses shared helper contracts
// instead of parent parser helpers, so language-owned adapters do not create
// dispatcher import cycles.
package golang
