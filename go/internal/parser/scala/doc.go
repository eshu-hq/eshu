// Package scala extracts Scala source facts for the parent parser engine.
//
// Parse returns classes, objects, traits, functions, variables, imports, calls,
// and bounded dead-code root metadata without importing the parent parser
// package. Root metadata is syntax-backed only; unresolved Scala semantics such
// as macros, implicits, givens, compiler plugins, dynamic dispatch, and route
// files remain exactness blockers for the query layer.
package scala
