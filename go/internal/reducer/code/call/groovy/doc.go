// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package groovy holds the Groovy call resolver: class-qualified,
// type-inferred callee resolution against the repository-unique name table.
// Groovy does not use the shared JVM imported-receiver resolver
// (code/call/jvm) because it has no import-bound receiver typing. It reads
// the shared entity index from code/call/shared and never imports code/call
// or its sibling language leaves.
package groovy
