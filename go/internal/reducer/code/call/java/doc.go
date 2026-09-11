// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package java holds the Java call resolver: it binds the shared JVM
// imported-receiver resolver (code/call/jvm) to Java's parser output
// (single "import" kind, ".java" source files matching the declared type's
// name), plus the file-root caller identity helper for
// service_loader/spring_autoconfiguration references. It reads the shared
// entity index from code/call/shared and never imports code/call or its
// sibling language leaves other than code/call/jvm.
package java
