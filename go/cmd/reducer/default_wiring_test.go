// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type defaultWiringCase struct {
	name            string
	defaultValue    any
	productionValue any
	override        defaultWiringOverride
}

type defaultWiringOverride struct {
	reason string
	assert func(t *testing.T, defaultValue, productionValue any)
}

func TestProductionWiringConsumesCapabilityDefaults(t *testing.T) {
	graphOrphanSweepCfg := loadGraphOrphanSweepConfig(func(string) string { return "" })
	generationRetentionCfg := loadGenerationRetentionConfig(func(string) string { return "" })

	assertDefaultWiring(t, []defaultWiringCase{
		{
			name:            "graph orphan sweep labels",
			defaultValue:    orphanSweepLabelStrings(sourcecypher.DefaultOrphanSweepLabels()),
			productionValue: graphOrphanSweepCfg.Runner.Policy.Labels,
		},
		{
			name:            "generation retention policy",
			defaultValue:    reducerGenerationRetentionPolicy(postgres.DefaultGenerationRetentionPolicy()),
			productionValue: generationRetentionCfg.Runner.Policy,
		},
	})
}

func assertDefaultWiring(t *testing.T, cases []defaultWiringCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if reflect.DeepEqual(tc.defaultValue, tc.productionValue) {
				if strings.TrimSpace(tc.override.reason) != "" || tc.override.assert != nil {
					t.Fatalf("%s declares an intentional override, but production equals the default", tc.name)
				}
				return
			}
			if strings.TrimSpace(tc.override.reason) == "" || tc.override.assert == nil {
				t.Fatalf("%s production wiring drifted from capability default without an explicit tested override\n  default: %#v\n  production: %#v",
					tc.name, tc.defaultValue, tc.productionValue)
			}
			tc.override.assert(t, tc.defaultValue, tc.productionValue)
		})
	}
}

func orphanSweepLabelStrings(labels []sourcecypher.OrphanSweepLabel) []string {
	values := make([]string, 0, len(labels))
	for _, label := range labels {
		values = append(values, string(label))
	}
	return values
}

func reducerGenerationRetentionPolicy(policy postgres.GenerationRetentionPolicy) reducer.GenerationRetentionPolicy {
	return reducer.GenerationRetentionPolicy{
		MinSupersededGenerations: policy.MinSupersededGenerations,
		MaxSupersededAge:         policy.MaxSupersededAge,
		BatchGenerationLimit:     policy.BatchGenerationLimit,
		BatchRowLimit:            policy.BatchRowLimit,
		PolicyScope:              policy.PolicyScope,
		PolicyRevision:           policy.PolicyRevision,
	}
}
