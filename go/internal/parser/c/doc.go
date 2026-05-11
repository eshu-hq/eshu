// Package c parses C source files into Eshu parser payloads.
//
// Parse emits C functions, types, includes, macros, typedefs, variables, calls,
// and parser-backed dead-code root metadata for bounded C reachability cases.
// AnnotatePublicHeaderRoots is a parent-engine hook that marks functions
// declared by directly included local headers without scanning the full
// repository; static header prototypes stay private. The root metadata is
// suppressive parser evidence, not a complete C linker model: macro expansion,
// conditional compilation, transitive include graphs, and dynamic symbol lookup
// remain outside this package's exactness contract.
package c
