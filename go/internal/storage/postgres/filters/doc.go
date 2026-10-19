// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package filters holds the single shared copy of the fact-kind filter
// cleanup used by both the postgres root package and the tfstate subpackage
// (issue #8552).
//
// The root package imports the tfstate subpackage (compat_tfstate.go), so
// tfstate cannot import the root back. CleanFactKinds stayed identical by
// prose alone until this package gave both sides one importable source.
// A change to the filter is a single-source edit, and the package test
// fails when its behavior drifts.
package filters
