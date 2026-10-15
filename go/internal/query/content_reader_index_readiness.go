// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ErrContentSubstringIndexesNotReady aliases querycontract's error of the
// same name, which this value moved to (#6060) so a handler-family
// subpackage can compare against the exact same instance with errors.Is.
var ErrContentSubstringIndexesNotReady = querycontract.ErrContentSubstringIndexesNotReady

func contentSubstringIndexReadError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55000" &&
		strings.Contains(pgErr.Message, "content substring indexes are not ready") {
		return ErrContentSubstringIndexesNotReady
	}
	return err
}

// isReaderMemberLoss accepts only a guarded runtime marker for a lost physical
// reader; generic PostgreSQL failures and caller cancellation are not retried.
func isReaderMemberLoss(err error) bool {
	var lost interface{ ReaderMemberLost() bool }
	return errors.As(err, &lost) && lost.ReaderMemberLost()
}

// recordCodeTopicSnapshotExportDenial marks a #7957 refusal on the
// postgres.query span and returns err unchanged. The reader build is not
// qualified for shared-snapshot export; the refusal happened before any probe,
// and it is permanent, so InvestigateCodeTopic returns it instead of running
// the single statement, which would hide the misconfiguration behind a slower
// answer.
func recordCodeTopicSnapshotExportDenial(span trace.Span, err error) error {
	span.SetAttributes(
		attribute.String("code_topic.execution_mode", "denied"),
		attribute.String("code_topic.denial_reason", "snapshot_export_unqualified"),
	)
	span.RecordError(err)
	span.SetStatus(codes.Error, "snapshot_export_unqualified")
	return err
}
