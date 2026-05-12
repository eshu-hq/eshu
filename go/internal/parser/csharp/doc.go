// Package csharp owns C# parser extraction without depending on the parent
// parser dispatcher. Parse emits declarations, calls, inheritance metadata, and
// bounded dead-code root hints for directly visible C# runtime and framework
// entrypoints. Root hints are syntax-scoped to declarations, attributes,
// modifiers, base lists, and same-file interface contracts; cross-project
// reflection, dependency injection, and generated code remain query-layer
// exactness blockers.
package csharp
