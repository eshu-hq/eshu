// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_head

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const containerImageIdentityPerfHeadVariant = true

func containerImageIdentityPerfPrepareIntent(intent *reducer.Intent) {
	intent.ClaimEpoch = 1
}

func containerImageIdentityPerfWriter(
	database db.ExecQueryer,
) reducer.ContainerImageIdentityWriter {
	beginner, ok := database.(db.Beginner)
	if !ok {
		return nil
	}
	cutoverStore := postgres.NewContainerImageIdentityCutoverStore(database)
	return reducer.PostgresContainerImageIdentityWriter{
		DB:                  database,
		CutoverLookup:       cutoverStore,
		LegacyCleanupLookup: cutoverStore,
		ClaimedExecer:       postgres.ContainerImageIdentityClaimedExecer{DB: database},
		Beginner: postgres.ContainerImageIdentityBeginner{
			Beginner: beginner,
		},
	}
}
