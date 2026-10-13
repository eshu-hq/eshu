// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package x continues the five-package cycle fixture for #7347.
package x

import "example.com/cycles/five/pkg/y"

// X calls into package y.
func X() { y.Y() }
