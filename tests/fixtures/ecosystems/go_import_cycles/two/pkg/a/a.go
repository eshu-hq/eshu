// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package a is one half of the two-package cycle fixture for #7347:
// a -> b -> a. A second file (a2.go) in the same package proves the
// package-level collapse reports one cycle, not one per file pair.
package a

import "example.com/cycles/two/pkg/b"

// A calls into package b.
func A() { b.B() }
