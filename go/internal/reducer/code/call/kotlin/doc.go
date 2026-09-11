// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kotlin holds the Kotlin call resolver: it binds the shared JVM
// imported-receiver resolver (code/call/jvm) to Kotlin's parser output
// (both plain and aliased imports introduce types, ".kt" source files, and
// no filename-matching requirement since Kotlin allows a type to live in
// any file). It reads the shared entity index from code/call/shared and
// never imports code/call or its sibling language leaves other than
// code/call/jvm.
package kotlin
