//go:build perf5854_head

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const containerImageIdentityPerfHeadVariant = true

func containerImageIdentityPerfWriter(
	database postgres.ExecQueryer,
) reducer.ContainerImageIdentityWriter {
	beginner, ok := database.(postgres.Beginner)
	if !ok {
		return nil
	}
	return reducer.PostgresContainerImageIdentityWriter{
		DB:            database,
		CutoverLookup: postgres.NewContainerImageIdentityCutoverStore(database),
		Beginner: postgres.ContainerImageIdentityBeginner{
			Beginner: beginner,
		},
	}
}
