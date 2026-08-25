// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	asktools "github.com/eshu-hq/eshu/go/internal/mcp/ask"
	cloudtools "github.com/eshu-hq/eshu/go/internal/mcp/cloud"
	doctools "github.com/eshu-hq/eshu/go/internal/mcp/documentation"
	playbooktools "github.com/eshu-hq/eshu/go/internal/mcp/playbooks"
	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
	visualizationtools "github.com/eshu-hq/eshu/go/internal/mcp/visualization"
)

// ToolDefinition describes one MCP tool exposed to clients.
//
// It aliases the dependency-neutral toolcontract definition so registration
// families can move below internal/mcp without importing their parent package.
type ToolDefinition = toolcontract.ToolDefinition

// ReadOnlyTools returns all read-only MCP tool definitions.
func ReadOnlyTools() []ToolDefinition {
	tools := make([]ToolDefinition, 0, 162)
	tools = append(tools, codebaseTools()...)
	tools = append(tools, codeFlowTools()...)
	tools = append(tools, repositoryLanguageTools()...)
	tools = append(tools, ecosystemTools()...)
	tools = append(tools, infraResourceAggregateTools()...)
	tools = append(tools, cloudInventoryTools()...)
	tools = append(tools, cloudRuntimeDriftTools()...)
	tools = append(tools, packageRegistryTools()...)
	tools = append(tools, admissionDecisionTools()...)
	tools = append(tools, packageRegistryAggregateTools()...)
	tools = append(tools, cicdTools()...)
	tools = append(tools, cicdRunCorrelationAggregateTools()...)
	tools = append(tools, serviceCatalogTools()...)
	tools = append(tools, codeownersTools()...)
	tools = append(tools, kubernetesTools()...)
	tools = append(tools, secretsIAMTools()...)
	tools = append(tools, observabilityCoverageTools()...)
	tools = append(tools, supplyChainTools()...)
	tools = append(tools, supplyChainImpactAggregateTools()...)
	tools = append(tools, securityAlertReconciliationAggregateTools()...)
	tools = append(tools, containerImageIdentityAggregateTools()...)
	tools = append(tools, sbomAttestationAttachmentAggregateTools()...)
	tools = append(tools, incidentContextTools()...)
	tools = append(tools, workItemTools()...)
	tools = append(tools, visualizationTools()...)
	tools = append(tools, freshnessTools()...)
	tools = append(tools, contextTools()...)
	tools = append(tools, serviceIntelligenceTools()...)
	tools = append(tools, contentTools()...)
	tools = append(tools, documentationTools()...)
	tools = append(tools, queryPlaybookTools()...)
	tools = append(tools, investigationWorkflowTools()...)
	tools = append(tools, investigationPacketTools()...)
	tools = append(tools, semanticEvidenceTools()...)
	tools = append(tools, semanticSearchTools()...)
	tools = append(tools, documentationFindingAggregateTools()...)
	tools = append(tools, componentExtensionTools()...)
	tools = append(tools, collectorExtractionReadinessTools()...)
	tools = append(tools, factSchemaVersionTools()...)
	tools = append(tools, runtimeTools()...)
	tools = append(tools, reachabilityTools()...)
	tools = append(tools, askTools()...)
	tools = append(tools, []ToolDefinition{relationshipEdgesTool(), repositoryFilesTool()}...)
	return tools
}

// documentationTools preserves the root package's constructor name while the
// documentation package owns the registration definitions.
func documentationTools() []ToolDefinition {
	return doctools.Tools()
}

// documentationFindingAggregateTools preserves the root package's constructor
// name while the documentation package owns the registration definitions.
func documentationFindingAggregateTools() []ToolDefinition {
	return doctools.FindingAggregateTools()
}

// cloudInventoryTools preserves the root package's constructor name while the
// cloud package owns the inventory registration definition.
func cloudInventoryTools() []ToolDefinition {
	return cloudtools.InventoryTools()
}

// cloudRuntimeDriftTools preserves the root package's constructor name while
// the cloud package owns the runtime-drift registration definition.
func cloudRuntimeDriftTools() []ToolDefinition {
	return cloudtools.RuntimeDriftTools()
}

// queryPlaybookTools preserves the root package's constructor name while the
// playbooks package owns the registration definitions.
func queryPlaybookTools() []ToolDefinition {
	return playbooktools.Tools()
}

// visualizationTools preserves the root package's constructor name while the
// visualization package owns the registration definition.
func visualizationTools() []ToolDefinition {
	return visualizationtools.Tools()
}

// askTools preserves the root package's constructor name while the ask
// package owns the registration definition.
func askTools() []ToolDefinition {
	return asktools.Tools()
}
