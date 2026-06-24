// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordEmission records facts-emitted, warning, and freshness-lag telemetry for
// a completed scope generation. Every label is a bounded enum: fact kind, parent
// scope kind, warning kind, and outcome. No instrument receives a resource name,
// project id, label value, IAM member, URL, or credential name.
func (s *Source) recordEmission(
	ctx context.Context,
	scopeCfg ScopeConfig,
	envelopes []facts.Envelope,
	boundary gcpcloud.Boundary,
	observedAt time.Time,
) {
	counts := make(map[string]int, 3)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	for factKind, count := range counts {
		s.recordFactsEmitted(ctx, factKind, scopeCfg.ParentScopeKind, count)
	}
	s.recordFreshnessLag(ctx, scopeCfg.ParentScopeKind, boundary.ReadTime, observedAt)
}

func (s *Source) recordPage(ctx context.Context, kind gcpcloud.ParentScopeKind) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordPage(ctx, kind)
}

func (s *Source) recordPageTokenResume(ctx context.Context, kind gcpcloud.ParentScopeKind) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordPageTokenResume(ctx, kind)
}

func (s *Source) recordFactsEmitted(ctx context.Context, factKind string, kind gcpcloud.ParentScopeKind, count int) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordFactsEmitted(ctx, factKind, kind, count)
}

func (s *Source) recordWarning(ctx context.Context, warningKind, outcome string) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordWarning(ctx, warningKind, outcome)
}

func (s *Source) recordClaim(ctx context.Context, status string) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordClaim(ctx, status)
}

// recordFreshnessLag records the lag from the provider read time to the Eshu
// observation time. A zero read time or negative lag is skipped to keep the
// histogram meaningful.
func (s *Source) recordFreshnessLag(ctx context.Context, kind gcpcloud.ParentScopeKind, readTime, observedAt time.Time) {
	if s.Metrics == nil || readTime.IsZero() {
		return
	}
	s.Metrics.RecordFreshnessLag(ctx, kind, observedAt.Sub(readTime).Seconds())
}

// logScope emits one structured line per committed scope generation. It logs
// bounded counts only; it never logs resource identities, labels, or credential
// names.
func (s *Source) logScope(ctx context.Context, scopeCfg ScopeConfig, generation *gcpcloud.Generation) {
	if s.Logger == nil {
		return
	}
	s.Logger.InfoContext(
		ctx, "gcp collector scope collected",
		slog.String("parent_scope_kind", string(scopeCfg.ParentScopeKind)),
		slog.String("asset_type_family", scopeCfg.AssetTypeFamily),
		slog.String("content_family", scopeCfg.ContentFamily),
		slog.Int("page_count", generation.PageCount()),
		slog.Int("resource_count", generation.ResourceCount()),
		slog.Int("warning_count", generation.WarningCount()),
	)
}
