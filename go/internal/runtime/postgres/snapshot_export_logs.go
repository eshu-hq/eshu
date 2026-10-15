// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"log/slog"
)

// Snapshot-export log events (#7957). They carry the reader's build identity
// and the operator's declared evidence, never a DSN, SQL text, or credential.
const (
	snapshotExportDeclarationEvent   = "postgres.reader.snapshot_export_declaration"
	snapshotExportQualificationEvent = "postgres.reader.snapshot_export_qualification"
	snapshotExportDeniedEvent        = "postgres.reader.snapshot_export_denied"
)

// logSnapshotExportDeclarations warns once at Open for every declaration whose
// server_version is a bare release label. Such a declaration matches every
// build that reports that label, patched or not, so it cannot detect an image
// pin that regresses to an unpatched build.
func logSnapshotExportDeclarations(logger *slog.Logger, builds []SnapshotExportBuild) {
	if logger == nil {
		logger = slog.Default()
	}
	for _, build := range builds {
		if !bareServerVersionLabel.MatchString(build.ServerVersion) {
			continue
		}
		logger.Warn("snapshot export declaration matches every build with this label and cannot detect an image-pin regression; set --with-extra-version on the qualified image",
			slog.String("event_name", snapshotExportDeclarationEvent),
			slog.String("server_version", build.ServerVersion),
			slog.String("evidence", build.Evidence),
		)
	}
}
