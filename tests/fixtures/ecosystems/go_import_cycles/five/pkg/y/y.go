// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package y continues the five-package cycle fixture for #7347.
package y

import "example.com/cycles/five/pkg/z"

// Y calls into package z.
func Y() { z.Z() }
