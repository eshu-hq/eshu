//go:build perf5854_main

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const containerImageIdentityPerfHeadVariant = false

func containerImageIdentityPerfWriter(
	database postgres.ExecQueryer,
) reducer.ContainerImageIdentityWriter {
	return reducer.PostgresContainerImageIdentityWriter{DB: database}
}
