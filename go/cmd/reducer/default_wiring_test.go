// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/retention"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/sweep/orphan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
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
			defaultValue:    orphanSweepLabelStrings(orphan.DefaultOrphanSweepLabels()),
			productionValue: graphOrphanSweepCfg.Runner.Policy.Labels,
		},
		{
			name:            "generation retention policy",
			defaultValue:    reducerGenerationRetentionPolicy(postgres.DefaultGenerationRetentionPolicy()),
			productionValue: generationRetentionCfg.Runner.Policy,
			override: defaultWiringOverride{
				reason: "the relationship retention sweep (#10823) is a reducer setting, not part of the " +
					"storage retention policy: it stays off, and its per-pass row cap defaults to " +
					"relationshipretention.DefaultRowCap",
				assert: assertRelationshipSweepIsTheOnlyDifference,
			},
		},
	})
}

// assertRelationshipSweepIsTheOnlyDifference accepts a production policy that
// equals the storage capability default except for the relationship sweep's row
// cap, which must be the leaf's default. The sweep flag is not set here: it is
// false in both, so any other difference (including the flag turning on by
// default) fails the comparison.
func assertRelationshipSweepIsTheOnlyDifference(t *testing.T, defaultValue, productionValue any) {
	t.Helper()
	want, ok := defaultValue.(retention.Policy)
	if !ok {
		t.Fatalf("default value is %T, want retention.Policy", defaultValue)
	}
	want.RelationshipRetentionRowCap = relationshipretention.DefaultRowCap
	if !reflect.DeepEqual(want, productionValue) {
		t.Fatalf("production policy differs from the capability default by more than the relationship row cap %d\n  want: %#v\n  got:  %#v",
			relationshipretention.DefaultRowCap, want, productionValue)
	}
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

func orphanSweepLabelStrings(labels []orphan.OrphanSweepLabel) []string {
	values := make([]string, 0, len(labels))
	for _, label := range labels {
		values = append(values, string(label))
	}
	return values
}

func reducerGenerationRetentionPolicy(policy postgres.GenerationRetentionPolicy) retention.Policy {
	return retention.Policy{
		MinSupersededGenerations: policy.MinSupersededGenerations,
		MaxSupersededAge:         policy.MaxSupersededAge,
		HardMaxSupersededAge:     policy.HardMaxSupersededAge,
		BatchGenerationLimit:     policy.BatchGenerationLimit,
		BatchRowLimit:            policy.BatchRowLimit,
		PolicyScope:              policy.PolicyScope,
		PolicyRevision:           policy.PolicyRevision,
	}
}
