// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"fmt"
	"slices"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (s *Source) listReferences(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
) ([]string, bool, error) {
	if len(target.References) > 0 {
		return append([]string(nil), target.References...), false, nil
	}
	var tags []string
	err := s.recordAPICall(ctx, target, "list_tags", func(context.Context) error {
		var err error
		tags, err = client.ListTags(ctx, target.Repository)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("list OCI registry tags: %w", err)
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	truncated := len(tags) > target.TagLimit
	if truncated {
		tags = tags[:target.TagLimit]
	}
	if s.Instruments != nil {
		s.Instruments.OCIRegistryTagsObserved.Add(ctx, int64(len(tags)), metric.WithAttributes(
			telemetry.AttrProvider(string(target.Provider)),
			telemetry.AttrResult("success"),
		))
	}
	return tags, truncated, nil
}
