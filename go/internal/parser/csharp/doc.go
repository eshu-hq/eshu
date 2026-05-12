// Package csharp owns C# parser extraction without depending on the parent
// parser dispatcher. Parse emits declarations, calls, inheritance metadata, and
// bounded dead-code root hints for directly visible C# runtime and framework
// entrypoints. Root hints are syntax-scoped to declarations, attributes,
// modifiers, base lists, qualified enclosing types, and same-file interface
// contracts with method arity; cross-project reflection, dependency injection,
// and generated code remain query-layer exactness blockers.
package csharp
