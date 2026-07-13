// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"

type nornicDBPhaseGroupExecutor = storagenornicdb.PhaseGroupExecutor

func nornicDBDefaultEntityPhaseConcurrency() int {
	return storagenornicdb.DefaultEntityPhaseConcurrency()
}
