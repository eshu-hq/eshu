// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// persistedInfraLabels is an intentionally independent persistence oracle for
// the infrastructure aggregate surfaces. It must not import or call the query
// package's aggregate expression: otherwise one broken expression could
// produce both the observed response and its expected value.
var persistedInfraLabels = []string{
	"CloudResource",
	"K8sResource", "KustomizeOverlay",
	"TerraformResource", "TerraformStateResource", "TerraformModule",
	"TerraformVariable", "TerraformOutput", "TerraformDataSource",
	"TerraformProvider", "TerraformLocal", "TerraformBackend",
	"TerraformImport", "TerraformMovedBlock", "TerraformRemovedBlock",
	"TerraformCheck", "TerraformLockProvider", "TerraformBlock",
	"TerragruntConfig", "TerragruntDependency", "CloudFormationResource",
	"ArgoCDApplication", "ArgoCDApplicationSet",
	"CrossplaneXRD", "CrossplaneComposition",
	"HelmChart", "HelmValues",
}

type persistedProviderProperties struct {
	Provider     string
	SourceSystem string
}

type persistedAggregateReader interface {
	CountNodes(ctx context.Context, label string) (int64, error)
	ListProviderProperties(ctx context.Context, label string) ([]persistedProviderProperties, error)
}

type persistedAggregateCounts struct {
	InfraAWS              int64 `json:"infra_aws_count"`
	InfraGCP              int64 `json:"infra_gcp_count"`
	EcosystemRepositories int64 `json:"ecosystem_repo_count"`
	EcosystemWorkloads    int64 `json:"ecosystem_workload_count"`
}

func readPersistedAggregateCounts(ctx context.Context, reader persistedAggregateReader) (persistedAggregateCounts, error) {
	repositories, err := reader.CountNodes(ctx, "Repository")
	if err != nil {
		return persistedAggregateCounts{}, fmt.Errorf("count persisted Repository nodes: %w", err)
	}
	workloads, err := reader.CountNodes(ctx, "Workload")
	if err != nil {
		return persistedAggregateCounts{}, fmt.Errorf("count persisted Workload nodes: %w", err)
	}

	counts := persistedAggregateCounts{
		EcosystemRepositories: repositories,
		EcosystemWorkloads:    workloads,
	}
	for _, label := range persistedInfraLabels {
		rows, err := reader.ListProviderProperties(ctx, label)
		if err != nil {
			return persistedAggregateCounts{}, fmt.Errorf("read persisted %s provider properties: %w", label, err)
		}
		for _, row := range rows {
			provider := strings.TrimSpace(row.Provider)
			if label == "CloudResource" && provider == "" {
				provider = strings.TrimSpace(row.SourceSystem)
			}
			switch provider {
			case "aws":
				counts.InfraAWS++
			case "gcp":
				counts.InfraGCP++
			}
		}
	}
	if counts.InfraAWS <= 0 || counts.InfraGCP <= 0 || counts.EcosystemRepositories <= 0 || counts.EcosystemWorkloads <= 0 {
		return persistedAggregateCounts{}, fmt.Errorf("persisted aggregate oracle requires positive counts, got %+v", counts)
	}
	return counts, nil
}

func marshalPersistedAggregateCounts(counts persistedAggregateCounts) ([]byte, error) {
	if counts.InfraAWS <= 0 || counts.InfraGCP <= 0 || counts.EcosystemRepositories <= 0 || counts.EcosystemWorkloads <= 0 {
		return nil, fmt.Errorf("persisted aggregate oracle requires positive counts, got %+v", counts)
	}
	raw, err := json.Marshal(counts)
	if err != nil {
		return nil, fmt.Errorf("marshal persisted aggregate counts: %w", err)
	}
	return append(raw, '\n'), nil
}

func (b *boltGraphCounter) ListProviderProperties(ctx context.Context, label string) ([]persistedProviderProperties, error) {
	if !identRE.MatchString(label) {
		return nil, fmt.Errorf("unsafe node label %q", label)
	}
	result, err := neo4j.ExecuteQuery(ctx, b.driver,
		fmt.Sprintf("MATCH (n:%s) RETURN n.provider AS provider, n.source_system AS source_system", label),
		nil, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, fmt.Errorf("list provider properties: %w", err)
	}
	rows := make([]persistedProviderProperties, 0, len(result.Records))
	for _, record := range result.Records {
		provider, _ := record.Get("provider")
		sourceSystem, _ := record.Get("source_system")
		rows = append(rows, persistedProviderProperties{
			Provider:     boltPropertyString(provider),
			SourceSystem: boltPropertyString(sourceSystem),
		})
	}
	return rows, nil
}
