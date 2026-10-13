// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package b continues the three-package cycle fixture for #7347.
package b

import "example.com/cycles/three/pkg/c"

// B calls into package c.
func B() { c.C() }
