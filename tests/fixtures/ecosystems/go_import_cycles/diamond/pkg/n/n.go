// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package n is the left arm of the diamond negative fixture for #7347.
package n

import "example.com/cycles/diamond/pkg/p"

// N calls into package p.
func N() { p.P() }
