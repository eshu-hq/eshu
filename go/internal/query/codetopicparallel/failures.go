// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetopicparallel

import (
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// IsReaderMemberLoss accepts only the guarded runtime marker for a lost
// physical reader member. Generic PostgreSQL failures and caller cancellation
// are not retried by the caller's whole-read retry.
func IsReaderMemberLoss(err error) bool {
	var lost interface{ ReaderMemberLost() bool }
	return errors.As(err, &lost) && lost.ReaderMemberLost()
}

// RecordSnapshotExportDenial marks a #7957 refusal on the caller's
// postgres.query span and returns err unchanged. The reader build is not
// qualified for shared-snapshot export; the refusal happened before any probe
// and is permanent, so the caller returns it instead of running the single
// statement, which would hide the misconfiguration behind a slower answer.
func RecordSnapshotExportDenial(span trace.Span, err error) error {
	span.SetAttributes(
		attribute.String("code_topic.execution_mode", "denied"),
		attribute.String("code_topic.denial_reason", "snapshot_export_unqualified"),
	)
	span.RecordError(err)
	span.SetStatus(codes.Error, "snapshot_export_unqualified")
	return err
}
