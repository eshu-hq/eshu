// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
)

type fakePersistedAggregateReader struct {
	nodes     map[string]int64
	providers map[string][]persistedProviderProperties
	err       error
}

func (f fakePersistedAggregateReader) CountNodes(_ context.Context, label string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.nodes[label], nil
}

func (f fakePersistedAggregateReader) ListProviderProperties(_ context.Context, label string) ([]persistedProviderProperties, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.providers[label], nil
}

func TestReadPersistedAggregateCountsUsesIndependentGraphProperties(t *testing.T) {
	t.Parallel()
	reader := fakePersistedAggregateReader{
		nodes: map[string]int64{"Repository": 30, "Workload": 9},
		providers: map[string][]persistedProviderProperties{
			"CloudResource": {
				{Provider: "", SourceSystem: "aws"},
				{Provider: "gcp", SourceSystem: "aws"},
			},
			"TerraformResource": {
				{Provider: "aws"},
				{Provider: "gcp"},
			},
		},
	}

	got, err := readPersistedAggregateCounts(context.Background(), reader)
	if err != nil {
		t.Fatalf("readPersistedAggregateCounts() error = %v", err)
	}
	want := persistedAggregateCounts{InfraAWS: 2, InfraGCP: 2, EcosystemRepositories: 30, EcosystemWorkloads: 9}
	if got != want {
		t.Fatalf("readPersistedAggregateCounts() = %+v, want %+v", got, want)
	}
}

func TestReadPersistedAggregateCountsFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reader fakePersistedAggregateReader
	}{
		{name: "missing repository", reader: fakePersistedAggregateReader{nodes: map[string]int64{"Workload": 9}}},
		{name: "missing workload", reader: fakePersistedAggregateReader{nodes: map[string]int64{"Repository": 30}}},
		{name: "missing aws", reader: fakePersistedAggregateReader{nodes: map[string]int64{"Repository": 30, "Workload": 9}, providers: map[string][]persistedProviderProperties{"TerraformResource": {{Provider: "gcp"}}}}},
		{name: "missing gcp", reader: fakePersistedAggregateReader{nodes: map[string]int64{"Repository": 30, "Workload": 9}, providers: map[string][]persistedProviderProperties{"TerraformResource": {{Provider: "aws"}}}}},
		{name: "reader error", reader: fakePersistedAggregateReader{err: errors.New("graph unavailable")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := readPersistedAggregateCounts(context.Background(), test.reader); err == nil {
				t.Fatal("readPersistedAggregateCounts() error = nil, want fail-closed error")
			}
		})
	}
}

func TestPersistedAggregateCountsJSONIsStrictNumericContract(t *testing.T) {
	t.Parallel()
	got, err := marshalPersistedAggregateCounts(persistedAggregateCounts{
		InfraAWS: 8, InfraGCP: 110, EcosystemRepositories: 30, EcosystemWorkloads: 9,
	})
	if err != nil {
		t.Fatalf("marshalPersistedAggregateCounts() error = %v", err)
	}
	want := "{\"infra_aws_count\":8,\"infra_gcp_count\":110,\"ecosystem_repo_count\":30,\"ecosystem_workload_count\":9}\n"
	if string(got) != want {
		t.Fatalf("marshalPersistedAggregateCounts() = %q, want %q", got, want)
	}
}

func TestParseFlagsEnablesPersistedAggregateComputeMode(t *testing.T) {
	t.Parallel()
	got, err := parseFlags([]string{"-print-persisted-aggregate-counts"})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if !got.printPersistedAggregates {
		t.Fatal("printPersistedAggregates = false, want true")
	}
}
