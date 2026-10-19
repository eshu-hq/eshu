// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fixtures holds the single shared copy of the drift-evidence test
// helpers used by both the postgres root package and the tfstate subpackage
// (issue #8552): the parser-shape config fixture builders and the queue-fake
// row padding. Production filter cleanup lives in the sibling filters
// package. A change to any helper is a single-source edit, and the package
// tests fail when its behavior drifts.
package fixtures
