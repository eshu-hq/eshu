package tfstateruntime

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (s ClaimedSource) recordSnapshotObserved(
	ctx context.Context,
	backendKind terraformstate.BackendKind,
	result string,
) {
	if s.Instruments == nil || s.Instruments.TerraformStateSnapshotsObserved == nil {
		return
	}
	s.Instruments.TerraformStateSnapshotsObserved.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrBackendKind(string(backendKind)),
		telemetry.AttrResult(result),
	))
}

func (s ClaimedSource) recordSnapshotBytes(
	ctx context.Context,
	backendKind terraformstate.BackendKind,
	size int64,
) {
	if s.Instruments == nil || s.Instruments.TerraformStateSnapshotBytes == nil || size < 0 {
		return
	}
	s.Instruments.TerraformStateSnapshotBytes.Record(ctx, size, metric.WithAttributes(
		telemetry.AttrBackendKind(string(backendKind)),
	))
}

func (s ClaimedSource) recordResourceFacts(
	ctx context.Context,
	backendKind terraformstate.BackendKind,
	count int64,
) {
	if s.Instruments == nil || s.Instruments.TerraformStateResourcesEmitted == nil || count <= 0 {
		return
	}
	s.Instruments.TerraformStateResourcesEmitted.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrBackendKind(string(backendKind)),
	))
}

func (s ClaimedSource) recordRedactions(ctx context.Context, redactions map[string]int64) {
	if s.Instruments == nil || s.Instruments.TerraformStateRedactionsApplied == nil {
		return
	}
	for reason, count := range redactions {
		reason = strings.TrimSpace(reason)
		if reason == "" || count <= 0 {
			continue
		}
		s.Instruments.TerraformStateRedactionsApplied.Add(ctx, count, metric.WithAttributes(
			telemetry.AttrReason(reason),
		))
	}
}

func (s ClaimedSource) recordS3NotModified(ctx context.Context, backendKind terraformstate.BackendKind) {
	if backendKind != terraformstate.BackendS3 ||
		s.Instruments == nil ||
		s.Instruments.TerraformStateS3ConditionalGetNotModified == nil {
		return
	}
	s.Instruments.TerraformStateS3ConditionalGetNotModified.Add(ctx, 1)
}

func (s ClaimedSource) recordParseDuration(
	ctx context.Context,
	backendKind terraformstate.BackendKind,
	duration time.Duration,
) {
	if s.Instruments == nil || s.Instruments.TerraformStateParseDuration == nil {
		return
	}
	s.Instruments.TerraformStateParseDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		telemetry.AttrBackendKind(string(backendKind)),
	))
}
