// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"errors"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/decode"
)

// LogEvidenceDecodeDrop emits an operator-diagnosable debug log for
// a work-item evidence fact dropped from a read because its payload failed
// typed decode. Moved here from internal/query/workitem/evidence.go (#6623),
// absorbing the verbatim fork the incident store carried. This package never
// logs on its own: each read path decides to call
// this helper when it drops a row.
func LogEvidenceDecodeDrop(err error) {
	var decodeErr *decode.Error
	if !errors.As(err, &decodeErr) {
		slog.Debug("work-item evidence fact dropped from list: decode error", slog.String("error", err.Error()))
		return
	}
	attrs := []any{
		slog.String("fact_id", decodeErr.FactID),
		slog.String("fact_kind", decodeErr.FactKind),
		slog.String("classification", decodeErr.Classification),
	}
	if decodeErr.Field != "" {
		attrs = append(attrs, slog.String("missing_field", decodeErr.Field))
	}
	slog.Debug("work-item evidence fact dropped from list", attrs...)
}
