// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package c closes the three-package cycle fixture for #7347:
// a -> b -> c -> a.
package c

import "example.com/cycles/three/pkg/a"

// C calls back into package a, closing the cycle.
func C() { a.A() }
