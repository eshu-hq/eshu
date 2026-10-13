// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package p is the sink of the diamond negative fixture for #7347.
package p

import "fmt"

// P only uses the standard library, so the diamond stays acyclic.
func P() { fmt.Println("p") }
