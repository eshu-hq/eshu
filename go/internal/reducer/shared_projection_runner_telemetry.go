// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordSharedProjectionStepDurations forwards to
// [worker.RecordStepDurations].
func recordSharedProjectionStepDurations(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain string,
	result PartitionProcessResult,
) {
	worker.RecordStepDurations(ctx, instruments, domain, result)
}
