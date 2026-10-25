// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package declaration projects runtime_declaration.version facts into the
// canonical graph as Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration (issue
// #8816).
//
// One RuntimeDeclaration node exists per declared runtime version, keyed on
// (repo_id, relative_path, line_number, runtime, order_index): a pin-file line
// that lists several versions holds one node per token. Handler retracts the
// declarations a generation supersedes and then writes the generation's facts.
// A full generation retracts each repository whole; a delta generation
// retracts only the files it changed or deleted, because its facts cover only
// those files. Nullable version parts stay null in the graph; aliases and
// unresolved specs are never turned into numbers.
//
// The package is a leaf below the reducer root: the root's domain catalog
// attaches Handler to Definition, and cmd/reducer wires the storage/cypher
// RuntimeDeclarationWriter as the Writer port.
package declaration
