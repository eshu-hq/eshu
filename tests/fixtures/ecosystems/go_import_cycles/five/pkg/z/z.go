// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package z closes the five-package cycle fixture for #7347:
// v -> w -> x -> y -> z -> v.
package z

import "example.com/cycles/five/pkg/v"

// Z calls back into package v, closing the cycle.
func Z() { v.V() }
