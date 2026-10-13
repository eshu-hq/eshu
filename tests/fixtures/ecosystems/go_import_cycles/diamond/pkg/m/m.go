// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package m opens the diamond negative fixture for #7347: m imports n and
// o, both import p, and nothing imports back, so there is no cycle.
package m

import (
	"example.com/cycles/diamond/pkg/n"
	"example.com/cycles/diamond/pkg/o"
)

// M calls into packages n and o.
func M() { n.N(); o.O() }
