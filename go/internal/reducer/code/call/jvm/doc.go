// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package jvm holds the shared imported-receiver resolver used by the java
// and kotlin call resolvers: it binds a receiver-typed call to the
// declaration of an imported type when the import resolves to exactly one
// repository path, then falls back to repository-scoped type-inference
// candidate names. groovy does not use this resolver (it has no JVM receiver
// import binding). It reads the shared entity index from code/call/shared
// and never imports code/call, code/call/java, or code/call/kotlin.
package jvm
