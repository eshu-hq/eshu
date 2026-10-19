// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func recordGCPMaterializationFact(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain reducercontract.Domain,
	factKind string,
	count int,
) {
	if instruments == nil || instruments.GCPMaterializationFacts == nil || count <= 0 {
		return
	}
	instruments.GCPMaterializationFacts.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrDomain(string(domain)),
		telemetry.AttrFactKind(factKind),
	))
}

func recordGCPMaterializationGraphWrites(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain reducercontract.Domain,
	kind string,
	count int,
) {
	if instruments == nil || instruments.GCPMaterializationGraphWrites == nil || count <= 0 {
		return
	}
	instruments.GCPMaterializationGraphWrites.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrDomain(string(domain)),
		telemetry.AttrKind(kind),
	))
}

func recordGCPMaterializationDuration(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain reducercontract.Domain,
	phase string,
	duration time.Duration,
) {
	if instruments == nil || instruments.GCPMaterializationDuration == nil {
		return
	}
	instruments.GCPMaterializationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		telemetry.AttrDomain(string(domain)),
		telemetry.AttrWritePhase(phase),
	))
}
