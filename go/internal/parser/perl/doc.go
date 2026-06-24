// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package perl parses Perl source evidence for the parent parser engine.
//
// Parse reads one Perl source file through a static tree-sitter grammar and
// emits the legacy parser payload buckets for packages, imports, subroutines,
// variables, and function calls. It also marks bounded dead-code roots for
// public packages, Exporter declarations, script entrypoints, constructors,
// special blocks, AUTOLOAD, and DESTROY. PreScan returns declaration names from
// the same payload path. The package is deterministic and does not import the
// parent parser package.
package perl
