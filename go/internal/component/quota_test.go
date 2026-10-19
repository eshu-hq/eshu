// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestManifestQuotaAbsentMeansUnlimited(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if limit, ok := manifest.Spec.Quotas.GenerationsPerHourLimit(); ok {
		t.Fatalf("GenerationsPerHourLimit() = (%d, true), want (_, false) for absent quotas", limit)
	}
	if limit, ok := manifest.Spec.Quotas.FactsPerGenerationLimit(); ok {
		t.Fatalf("FactsPerGenerationLimit() = (%d, true), want (_, false) for absent quotas", limit)
	}
}

func TestManifestQuotaAcceptsPositiveLimits(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.Quotas = Quota{MaxFactsPerGeneration: intPtr(5000), MaxGenerationsPerHour: intPtr(24)}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if limit, ok := manifest.Spec.Quotas.GenerationsPerHourLimit(); !ok || limit != 24 {
		t.Fatalf("GenerationsPerHourLimit() = (%d, %v), want (24, true)", limit, ok)
	}
	if limit, ok := manifest.Spec.Quotas.FactsPerGenerationLimit(); !ok || limit != 5000 {
		t.Fatalf("FactsPerGenerationLimit() = (%d, %v), want (5000, true)", limit, ok)
	}
}

func TestManifestQuotaRejectsNonPositiveLimits(t *testing.T) {
	t.Parallel()

	for _, value := range []int{0, -3} {
		manifest := validManifest()
		manifest.Spec.Quotas = Quota{MaxGenerationsPerHour: intPtr(value)}
		err := manifest.Validate()
		if err == nil {
			t.Fatalf("Validate() with maxGenerationsPerHour=%d error = nil, want rejection", value)
		}
		if !strings.Contains(err.Error(), "maxGenerationsPerHour") {
			t.Fatalf("Validate() error = %v, want maxGenerationsPerHour error", err)
		}

		manifest = validManifest()
		manifest.Spec.Quotas = Quota{MaxFactsPerGeneration: intPtr(value)}
		if err := manifest.Validate(); err == nil {
			t.Fatalf("Validate() with maxFactsPerGeneration=%d error = nil, want rejection", value)
		}
	}
}

func TestQuotaResolveOperatorOverrideWinsPerField(t *testing.T) {
	t.Parallel()

	manifest := Quota{MaxFactsPerGeneration: intPtr(5000), MaxGenerationsPerHour: intPtr(24)}
	override := Quota{MaxGenerationsPerHour: intPtr(6)}
	effective := manifest.Resolve(override)
	if limit, ok := effective.GenerationsPerHourLimit(); !ok || limit != 6 {
		t.Fatalf("effective GenerationsPerHourLimit() = (%d, %v), want (6, true)", limit, ok)
	}
	if limit, ok := effective.FactsPerGenerationLimit(); !ok || limit != 5000 {
		t.Fatalf("effective FactsPerGenerationLimit() = (%d, %v), want (5000, true) (manifest kept)", limit, ok)
	}
}

func TestQuotaResolveEmptyOverrideKeepsManifest(t *testing.T) {
	t.Parallel()

	manifest := Quota{MaxGenerationsPerHour: intPtr(24)}
	effective := manifest.Resolve(Quota{})
	if limit, ok := effective.GenerationsPerHourLimit(); !ok || limit != 24 {
		t.Fatalf("effective GenerationsPerHourLimit() = (%d, %v), want (24, true)", limit, ok)
	}
}

func TestQuotaResolveOverrideAloneGrantsLimit(t *testing.T) {
	t.Parallel()

	effective := Quota{}.Resolve(Quota{MaxGenerationsPerHour: intPtr(10)})
	if limit, ok := effective.GenerationsPerHourLimit(); !ok || limit != 10 {
		t.Fatalf("effective GenerationsPerHourLimit() = (%d, %v), want (10, true)", limit, ok)
	}
}
