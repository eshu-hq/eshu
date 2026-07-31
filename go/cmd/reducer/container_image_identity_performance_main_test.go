// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_main

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const containerImageIdentityPerfHeadVariant = false

func containerImageIdentityPerfPrepareIntent(intent *reducer.Intent) {
	// The shared handler now validates the queue claim token before dispatching
	// either writer. The legacy writer does not consume it.
	intent.ClaimEpoch = 1
}

func containerImageIdentityPerfWriter(
	database postgres.ExecQueryer,
) reducer.ContainerImageIdentityWriter {
	store := postgres.NewFactStore(database)
	return containerImageIdentityPerfLegacyWriter{store: store}
}
