// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// requiredServerEncoding is the only server_encoding the read model accepts.
// row_digest hashes config_artifact_utf8(text), which is IMMUTABLE only
// because a database's encoding never changes; on any other encoding the
// stored digest would not be the digest of the UTF8 bytes the Go side reads.
const requiredServerEncoding = "UTF8"

// showServerEncodingSQL reads the database encoding. server_encoding is fixed
// when the database is created, so one read per process start is enough.
const showServerEncodingSQL = `SHOW server_encoding`

// Encoding is the result of the start-up encoding check (re-ruling 3).
// ServerEncoding is empty when the check could not read it. When Supported is
// false the caller skips every derive (outcome OutcomeEncodingUnsupported) and
// reports the read model as StatusNotInstalled with Reason; content Writes and
// live reads proceed unchanged.
type Encoding struct {
	ServerEncoding string
	Supported      bool
	Reason         string
}

// CheckEncoding runs SHOW server_encoding once and logs the value under the
// event postgres.server_encoding: at INFO for UTF8, at ERROR otherwise. It
// never fails the caller. A failed read is reported as unsupported with
// ReasonEncodingCheckFailed, so the read model stays off rather than storing
// digests the process cannot vouch for. A nil logger logs nothing.
func CheckEncoding(ctx context.Context, queryer db.Queryer, logger *slog.Logger) Encoding {
	value, err := readServerEncoding(ctx, queryer)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "postgres server encoding check failed; config artifact read model disabled",
				"event_name", "postgres.server_encoding",
				"config_artifacts_status", StatusNotInstalled,
				"reason", ReasonEncodingCheckFailed,
				"error", err.Error())
		}
		return Encoding{Reason: ReasonEncodingCheckFailed}
	}
	if value != requiredServerEncoding {
		if logger != nil {
			logger.ErrorContext(ctx, "postgres server encoding is not UTF8; config artifact read model disabled",
				"event_name", "postgres.server_encoding",
				"server_encoding", value,
				"required_encoding", requiredServerEncoding,
				"config_artifacts_status", StatusNotInstalled,
				"reason", ReasonEncodingUnsupported)
		}
		return Encoding{ServerEncoding: value, Reason: ReasonEncodingUnsupported}
	}
	if logger != nil {
		logger.InfoContext(ctx, "postgres server encoding",
			"event_name", "postgres.server_encoding",
			"server_encoding", value)
	}
	return Encoding{ServerEncoding: value, Supported: true}
}

func readServerEncoding(ctx context.Context, queryer db.Queryer) (string, error) {
	rows, err := queryer.QueryContext(ctx, showServerEncodingSQL)
	if err != nil {
		return "", fmt.Errorf("read postgres server_encoding: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("read postgres server_encoding: %w", err)
		}
		return "", fmt.Errorf("read postgres server_encoding: no row")
	}
	var value string
	if err := rows.Scan(&value); err != nil {
		return "", fmt.Errorf("scan postgres server_encoding: %w", err)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read postgres server_encoding: %w", err)
	}
	return value, nil
}
