// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semantic derives the type-system and value-flow half of Go
// dead-code root evidence, for the sibling internal/parser/golang/deadcode
// package to compose with its own registration and signature-shape evidence.
//
// CollectRoots walks a parsed file once to gather every declaration and
// resolution-candidate node (var/const specs, short variable declarations,
// assignments, composite literals, parameter and field declarations,
// function declarations, return statements, call expressions, and type
// parameter declarations), then resolves each gathered node in an in-memory
// loop instead of a repeated full-tree walk (issue #4920). Gathering runs
// pre-order, so a forward reference — a call naming a function declared
// later in the file, or a type parameter constraint naming an interface
// declared later — still resolves, because every declaration is known before
// any resolution loop runs.
//
// The evidence CollectRoots marks falls into four families: interface
// satisfaction (a concrete type reached an interface-typed return, struct
// field, or call argument, whether directly or by tracing an imported
// interface's required method set), function/method value references (a
// function or method passed or assigned as a value, including into a
// function-literal closure), generic-constraint method roots (a type
// parameter constrained by a known local interface), and
// dependency-injection callback arguments (an identifier or method value
// passed into a parameter position already known to accept a callback).
//
// This package never imports internal/parser/golang or its sibling
// internal/parser/golang/deadcode — either back-edge is exactly the import
// cycle issue #6774 removes. Anything it needs from the old flat package
// lives in internal/parser/golang/symbols.
package semantic
