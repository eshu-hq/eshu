// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package o is the right arm of the diamond negative fixture for #7347.
package o

import "example.com/cycles/diamond/pkg/p"

// O calls into package p.
func O() { p.P() }
