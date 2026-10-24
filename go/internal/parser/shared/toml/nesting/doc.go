// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package nesting bounds how deeply a TOML document nests before a decoder
// sees it.
//
// github.com/pelletier/go-toml/v2 v2.3.1 recurses once per array or inline
// table level and once per dotted-key segment, and it has no depth limit. A
// repository file of about 0.9 MB with nested '[' overflows the goroutine
// stack, which is a fatal runtime error that recover cannot catch, so one
// hostile file kills the whole ingester (#8985). Check runs one linear,
// allocation-free pass that measures that depth and returns ErrTooDeep past
// the caller's limit, or ErrUnterminatedString for a string that never
// closes. Callers report either as an unparseable file and keep going.
//
// The lexer skips comments and the four TOML string forms with the same
// rules as the go-toml scanner, including the one or two extra quotes a
// multi-line string may hold before its closing delimiter, so brackets,
// braces, and dots inside strings and comments never count.
package nesting
