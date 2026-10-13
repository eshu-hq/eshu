// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package b is the other half of the two-package cycle fixture for #7347:
// a -> b -> a.
package b

import "example.com/cycles/two/pkg/a"

// B calls back into package a, closing the cycle.
func B() { a.A() }
