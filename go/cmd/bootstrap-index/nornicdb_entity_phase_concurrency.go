// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

const (
	nornicDBEntityPhaseConcurrencyCap = 16
	nornicDBEntityPhaseConcurrencyEnv = "ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY"
)

// nornicDBDefaultEntityPhaseConcurrency matches the ingester's canonical
// entity-phase default so bootstrap and steady-state NornicDB writes use the
// same CPU-scaled worker budget.
func nornicDBDefaultEntityPhaseConcurrency() int {
	n := runtime.NumCPU()
	if n > nornicDBEntityPhaseConcurrencyCap {
		n = nornicDBEntityPhaseConcurrencyCap
	}
	if n < 1 {
		return 1
	}
	return n
}

func nornicDBEntityPhaseConcurrency(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(nornicDBEntityPhaseConcurrencyEnv))
	if raw == "" {
		return nornicDBDefaultEntityPhaseConcurrency(), nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf(
			"parse %s=%q: must be a positive integer",
			nornicDBEntityPhaseConcurrencyEnv,
			raw,
		)
	}
	if n > nornicDBEntityPhaseConcurrencyCap {
		return nornicDBEntityPhaseConcurrencyCap, nil
	}
	return n, nil
}
