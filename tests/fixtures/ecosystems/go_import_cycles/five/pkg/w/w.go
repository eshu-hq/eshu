// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package w continues the five-package cycle fixture for #7347.
package w

import "example.com/cycles/five/pkg/x"

// W calls into package x.
func W() { x.X() }
