// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v opens the five-package cycle fixture for #7347:
// v -> w -> x -> y -> z -> v.
package v

import "example.com/cycles/five/pkg/w"

// V calls into package w.
func V() { w.W() }
