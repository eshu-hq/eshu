package collector

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	scipSnapshotLanguageUnknown     = "unknown"
	scipSnapshotResultDisabled      = "disabled"
	scipSnapshotResultNoLanguage    = "no_supported_language"
	scipSnapshotResultBinaryMissing = "binary_unavailable"
	scipSnapshotResultIndexerFailed = "indexer_failed"
	scipSnapshotResultParseFailed   = "parse_failed"
	scipSnapshotResultEmpty         = "empty_result"
	scipSnapshotResultUsed          = "used"
)

func (s NativeRepositorySnapshotter) recordSCIPSnapshotAttempt(ctx context.Context, language string, result string) {
	if s.Instruments == nil {
		return
	}
	if strings.TrimSpace(language) == "" {
		language = scipSnapshotLanguageUnknown
	}
	s.Instruments.SCIPSnapshotAttempts.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrLanguage(language),
		telemetry.AttrResult(result),
	))
}
