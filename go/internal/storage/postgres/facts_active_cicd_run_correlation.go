// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ListActiveCICDRunCorrelationFacts loads active reducer-owned container image
// identity rows for the artifact digests or image refs observed in one CI/CD
// run generation.
func (s FactStore) ListActiveCICDRunCorrelationFacts(
	ctx context.Context,
	digests []string,
	imageRefs []string,
) ([]facts.Envelope, error) {
	return s.listCurrentContainerImageIdentitySupportFacts(ctx, containerImageIdentitySupportFactFilter{
		digests:   digests,
		imageRefs: imageRefs,
	})
}

func cleanStringFilterValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	slices.Sort(cleaned)
	return cleaned
}
