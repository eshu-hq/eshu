// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"

	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// runtimeDeclarationSkipReason is the closed reason logged when a pin file is
// skipped. It is a log field, not a metric label.
const runtimeDeclarationSkipReason = "token_cap_exceeded"

// pinSkipRecorder returns the onSkip callback the runtime declaration emitter
// calls for a pin file over runtimedeclaration.MaxTokensPerFile. It logs WARN
// git_runtime_declaration_file_skipped through the GitSource ctx carries. The
// skip is deterministic for the file content, so it does not mark the
// generation dropped. A ctx without a recorder or a logger makes the callback
// a no-op; the file emits no facts either way.
func pinSkipRecorder(ctx context.Context, repositoryID, scopeID, generationID string) func(relativePath string, tokenCount int) {
	return func(relativePath string, tokenCount int) {
		s, _ := ctx.Value(streamDropRecorderKey{}).(*GitSource)
		if s == nil || s.Logger == nil {
			return
		}
		s.Logger.WarnContext(ctx, "git_runtime_declaration_file_skipped",
			slog.String("reason", runtimeDeclarationSkipReason),
			slog.String("repository_id", repositoryID),
			slog.String(telemetry.LogKeyScopeID, scopeID),
			slog.String(telemetry.LogKeyGenerationID, generationID),
			slog.String("relative_path", relativePath),
			slog.Int("token_count", tokenCount),
			slog.Int("token_cap", runtimedeclaration.MaxTokensPerFile),
		)
	}
}
