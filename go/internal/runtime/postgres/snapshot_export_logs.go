// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
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

// logSnapshotExportQualification records, once at Open, whether each reader
// pool or fleet member may export a shared snapshot: INFO when qualified, WARN
// when every snapshot-set read it would serve will be refused. It binds the
// server_version, server_version_num, recovery state, and declared evidence
// an operator needs to tie the verdict to an image.
func (a *Access) logSnapshotExportQualification() {
	if len(a.readerMembers) == 0 {
		logSnapshotExportVerdict(context.Background(), a.logger, slog.LevelInfo, snapshotExportQualificationEvent, "", -1, a.snapshotExport)
		return
	}
	for i := range a.readerMembers {
		member := &a.readerMembers[i]
		logSnapshotExportVerdict(context.Background(), a.logger, slog.LevelInfo, snapshotExportQualificationEvent, "", member.ordinal, member.snapshotExport)
	}
}

// logSnapshotExportVerdict writes one qualification or denial record. A
// qualified verdict logs at level; an unqualified or unobserved one logs WARN.
// ordinal is the member's inventory ordinal, or negative for the single pool.
func logSnapshotExportVerdict(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, ordinal int, holder *snapshotExportQualification) {
	if logger == nil {
		logger = slog.Default()
	}
	verdict := holder.current()
	attrs := []slog.Attr{slog.String("event_name", event)}
	if ordinal >= 0 {
		attrs = append(attrs, slog.Int("member_ordinal", ordinal))
	}
	if verdict == nil {
		level = slog.LevelWarn
		attrs = append(attrs, slog.Bool("qualified", false), slog.String("reason", holder.denialReason()), slog.String("server_version", ""))
	} else {
		if !verdict.qualified {
			level = slog.LevelWarn
		}
		attrs = append(attrs,
			slog.Bool("qualified", verdict.qualified),
			slog.String("reason", verdict.reason),
			slog.String("server_version", verdict.serverVersion),
			slog.Int("server_version_num", verdict.serverVersionNum),
			slog.Bool("in_recovery", verdict.inRecovery),
			slog.String("evidence", verdict.evidence),
		)
	}
	if message == "" {
		message = "PostgreSQL reader snapshot export qualification"
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}
