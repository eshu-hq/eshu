// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package a is one half of the two-package cycle fixture for #7347.
package a

import "example.com/cycles/two/pkg/b"

// A2 also calls into package b from the same package.
func A2() { b.B() }
