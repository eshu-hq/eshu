// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import "testing"

func TestIngestionScopeValidateAllowsAdditionalCollectorKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceSystem  string
		collectorKind CollectorKind
	}{
		{
			name:          "aws",
			sourceSystem:  "aws",
			collectorKind: CollectorAWS,
		},
		{
			name:          "terraform_state",
			sourceSystem:  "terraform_state",
			collectorKind: CollectorTerraformState,
		},
		{
			name:          "webhook",
			sourceSystem:  "webhook",
			collectorKind: CollectorWebhook,
		},
		{
			name:          "documentation",
			sourceSystem:  "documentation",
			collectorKind: CollectorDocumentation,
		},
		{
			name:          "oci_registry",
			sourceSystem:  "oci_registry",
			collectorKind: CollectorOCIRegistry,
		},
		{
			name:          "package_registry",
			sourceSystem:  "package_registry",
			collectorKind: CollectorPackageRegistry,
		},
		{
			name:          "vulnerability_intelligence",
			sourceSystem:  "vulnerability_intelligence",
			collectorKind: CollectorVulnerabilityIntelligence,
		},
		{
			name:          "security_alert",
			sourceSystem:  "security_alert",
			collectorKind: CollectorSecurityAlert,
		},
		{
			name:          "ci_cd_run",
			sourceSystem:  "ci_cd_run",
			collectorKind: CollectorCICDRun,
		},
		{
			name:          "pagerduty",
			sourceSystem:  "pagerduty",
			collectorKind: CollectorPagerDuty,
		},
		{
			name:          "jira",
			sourceSystem:  "jira",
			collectorKind: CollectorJira,
		},
		{
			name:          "scanner_worker",
			sourceSystem:  "scanner_worker",
			collectorKind: CollectorScannerWorker,
		},
		{
			name:          "semantic_extraction",
			sourceSystem:  "semantic_extraction",
			collectorKind: CollectorSemanticExtraction,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scope := IngestionScope{
				ScopeID:       "scope-123",
				SourceSystem:  tt.sourceSystem,
				ScopeKind:     KindRepository,
				CollectorKind: tt.collectorKind,
				PartitionKey:  "partition-123",
			}

			if err := scope.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
