// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

// filterRowsByReadiness forwards to [worker.FilterRowsByReadiness].
func filterRowsByReadiness(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
) (readyRows, blockedRows, terminalRows []SharedProjectionIntentRow, err error) {
	return worker.FilterRowsByReadiness(ctx, domain, rows, readinessLookup, readinessPrefetch, endpointPresence)
}

// maxSharedIntentWaitSeconds forwards to [worker.MaxIntentWaitSeconds].
func maxSharedIntentWaitSeconds(now time.Time, rows []SharedProjectionIntentRow) float64 {
	return worker.MaxIntentWaitSeconds(now, rows)
}
