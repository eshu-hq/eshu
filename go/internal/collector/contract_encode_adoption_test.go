// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

func TestContractEncodeAdoptionRatchet(t *testing.T) {
	targets := []struct {
		path  string
		calls []string
	}{
		{
			path: "grafana/envelope.go",
			calls: []string{
				"EncodeObservabilitySourceInstance",
				"EncodeObservabilityObservedDashboard",
				"EncodeObservabilityObservedRule",
				"EncodeObservabilityCoverageWarning",
			},
		},
		{
			path: "loki/envelope.go",
			calls: []string{
				"EncodeObservabilitySourceInstance",
				"EncodeObservabilityObservedLogSignal",
				"EncodeObservabilityObservedRule",
				"EncodeObservabilityCoverageWarning",
			},
		},
		{
			path: "tempo/envelope.go",
			calls: []string{
				"EncodeObservabilitySourceInstance",
				"EncodeObservabilityObservedTraceSignal",
				"EncodeObservabilityCoverageWarning",
			},
		},
		{
			path: "prometheusmimir/envelope.go",
			calls: []string{
				"EncodeObservabilitySourceInstance",
				"EncodeObservabilityObservedTarget",
				"EncodeObservabilityObservedRule",
				"EncodeObservabilityCoverageWarning",
			},
		},
		{
			path: "ociregistry/envelope.go",
			calls: []string{
				"EncodeOCIRegistryRepository",
				"EncodeOCIImageManifest",
				"EncodeOCIImageIndex",
				"EncodeOCIImageDescriptor",
				"EncodeOCIImageTagObservation",
				"EncodeOCIImageReferrer",
			},
		},
		{
			path:  "ociregistry/warning.go",
			calls: []string{"EncodeOCIRegistryWarning"},
		},
		{
			path:  "packageregistry/envelope.go",
			calls: []string{"EncodePackageRegistryPackage"},
		},
		{
			path:  "packageregistry/version.go",
			calls: []string{"EncodePackageRegistryPackageVersion"},
		},
		{
			path:  "packageregistry/warning.go",
			calls: []string{"EncodePackageRegistryWarning"},
		},
		{
			path:  "terraformstate/parser.go",
			calls: []string{"EncodeTerraformStateSnapshot"},
		},
		{
			path:  "terraformstate/resources.go",
			calls: []string{"EncodeTerraformStateResource"},
		},
		{
			path:  "terraformstate/modules.go",
			calls: []string{"EncodeTerraformStateModule"},
		},
		{
			path:  "terraformstate/outputs.go",
			calls: []string{"EncodeTerraformStateOutput"},
		},
		{
			path:  "terraformstate/tags.go",
			calls: []string{"EncodeTerraformStateTagObservation"},
		},
		{
			path:  "terraformstate/providers.go",
			calls: []string{"EncodeTerraformStateProviderBinding"},
		},
		{
			path:  "terraformstate/warnings.go",
			calls: []string{"EncodeTerraformStateWarning"},
		},
		{
			path:  "terraformstate/warning_fact.go",
			calls: []string{"EncodeTerraformStateWarning"},
		},
		{
			path:  "tfstate_candidate.go",
			calls: []string{"EncodeTerraformStateCandidate"},
		},
		{
			path: "sbomdocument/cyclonedx_fixture.go",
			calls: []string{
				"EncodeSBOMDocument",
			},
		},
		{
			path: "sbomdocument/envelope.go",
			calls: []string{
				"EncodeSBOMDependencyRelationship",
				"EncodeSBOMExternalReference",
				"EncodeSBOMWarning",
			},
		},
		{
			path: "sbomruntime/attestation.go",
			calls: []string{
				"EncodeAttestationSignatureVerification",
				"EncodeAttestationStatement",
				"EncodeSBOMWarning",
			},
		},
		{
			path: "scannerworker/sbomgenerator/envelope.go",
			calls: []string{
				"EncodeSBOMComponent",
				"EncodeSBOMDocument",
				"EncodeSBOMWarning",
			},
		},
		{
			path: "cicdrun/github_actions_fixture.go",
			calls: []string{
				"EncodeCICDArtifact",
				"EncodeCICDEnvironmentObservation",
				"EncodeCICDRun",
				"EncodeCICDStep",
				"EncodeCICDTriggerEdge",
			},
		},
		{
			path:  "securityalerts/envelope.go",
			calls: []string{"EncodeSecurityAlertRepositoryAlert"},
		},
		{
			path: "jira/envelope.go",
			calls: []string{
				"EncodeWorkItemExternalLink",
				"EncodeWorkItemRecord",
				"EncodeWorkItemTransition",
			},
		},
		{
			path: "pagerduty/envelope.go",
			calls: []string{
				"EncodeChangeRecord",
				"EncodeIncidentLifecycleEvent",
				"EncodeIncidentRecord",
			},
		},
		{
			path: "pagerduty/config_envelope.go",
			calls: []string{
				"EncodeIncidentRoutingCoverageWarning",
				"EncodeIncidentRoutingObservedPagerDutyIntegration",
				"EncodeIncidentRoutingObservedPagerDutyService",
			},
		},
		{
			path: "terraformstate/pagerduty_applied.go",
			calls: []string{
				"EncodeIncidentRoutingAppliedAlertRoute",
				"EncodeIncidentRoutingAppliedPagerDutyResource",
				"EncodeIncidentRoutingCoverageWarning",
			},
		},
	}

	for _, target := range targets {
		t.Run(target.path, func(t *testing.T) {
			calls := selectorCalls(t, target.path)
			var missing []string
			for _, call := range target.calls {
				if !calls[call] {
					missing = append(missing, call)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				t.Fatalf("missing factschema encoder call(s): %s", strings.Join(missing, ", "))
			}
		})
	}
}

func selectorCalls(t *testing.T, path string) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	calls := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		calls[selector.Sel.Name] = true
		return true
	})
	return calls
}
