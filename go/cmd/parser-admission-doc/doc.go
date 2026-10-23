// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command parser-admission-doc regenerates the admission table in
// docs/public/languages/admitted-files.md from the parser registry.
//
// The table lists, for every registered parser definition, its ecosystem,
// admission level, extensions, exact file names, and file name prefixes. With
// -check it writes nothing and exits non-zero when the committed page differs
// from the registry, which is the drift gate. Output is deterministic.
package main
