// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package catalog parses a Gradle version catalog (gradle/libs.versions.toml)
// into dependency rows for the supply-chain impact reducer.
//
// The parser applies a small, documented grammar to the [versions],
// [libraries], [bundles] and [plugins] tables and emits a resolved row only
// when a construct matches it. Anything else is counted, never guessed: a
// version reference the file cannot satisfy becomes an unresolved row with no
// value, a dynamic version, range or other Gradle selector stays unresolved,
// and a construct outside the grammar yields no row. Aliases are matched the
// way Gradle normalizes them ('-', '_' and '.' are one separator), and an
// alias that collides with another in its table never resolves. See README.md
// for the grammar and the bounds on input size, nesting and row count.
//
// The package never executes Gradle, reads settings.gradle, or follows a
// catalog declared in another file.
package catalog
