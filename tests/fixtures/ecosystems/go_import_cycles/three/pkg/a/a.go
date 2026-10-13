// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package a opens the three-package cycle fixture for #7347:
// a -> b -> c -> a.
package a

import "example.com/cycles/three/pkg/b"

// A calls into package b.
func A() { b.B() }
