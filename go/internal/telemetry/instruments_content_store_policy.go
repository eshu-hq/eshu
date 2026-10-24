// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerContentStorePolicyInstruments registers the content-store policy
// counter (#8812). One add per (policy, outcome) per collected fact stream,
// where policy is metadata or redact and outcome is metadata (body withheld),
// redacted (at least one value replaced), clean (nothing to replace), or
// redactor_failed (the redactor errored or the body was not UTF-8, so the file
// became a metadata row; never the raw body). A rising redactor_failed count
// means a credential-bearing file is silently losing its body.
func registerContentStorePolicyInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ContentStorePolicyFiles, err = meter.Int64Counter(
		"eshu_dp_collector_content_store_files_total",
		metric.WithDescription("Files the git collector applied a non-store content-store policy to, by policy (metadata, redact) and outcome (metadata, redacted, clean, redactor_failed) (#8812)"),
	); err != nil {
		return fmt.Errorf("register ContentStorePolicyFiles counter: %w", err)
	}
	return nil
}
