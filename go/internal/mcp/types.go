// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	asktools "github.com/eshu-hq/eshu/go/internal/mcp/ask"
	cloudtools "github.com/eshu-hq/eshu/go/internal/mcp/cloud"
	doctools "github.com/eshu-hq/eshu/go/internal/mcp/documentation"
	freshnesstools "github.com/eshu-hq/eshu/go/internal/mcp/freshness"
	investigationtools "github.com/eshu-hq/eshu/go/internal/mcp/investigation"
	playbooktools "github.com/eshu-hq/eshu/go/internal/mcp/playbooks"
	relationshiptools "github.com/eshu-hq/eshu/go/internal/mcp/relationships"
	semantictools "github.com/eshu-hq/eshu/go/internal/mcp/semantic"
	servicetools "github.com/eshu-hq/eshu/go/internal/mcp/service"
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

// investigationWorkflowTools preserves the root package's constructor name
// while the investigation package owns the workflow registration definitions.
func investigationWorkflowTools() []ToolDefinition {
	return investigationtools.WorkflowTools()
}

// investigationPacketTools preserves the root package's constructor name
// while the investigation package owns the evidence-packet definitions.
func investigationPacketTools() []ToolDefinition {
	return investigationtools.PacketTools()
}

// visualizationTools preserves the root package's constructor name while the
// visualization package owns the registration definition.
func visualizationTools() []ToolDefinition {
	return visualizationtools.Tools()
}

// freshnessTools preserves the root package's constructor name while the
// freshness package owns the registration definitions.
func freshnessTools() []ToolDefinition {
	return freshnesstools.Tools()
}

// serviceCatalogTools preserves the root package's constructor name while the
// service package owns the catalog registration definition.
func serviceCatalogTools() []ToolDefinition {
	return servicetools.CatalogTools()
}

// contextTools preserves the root package's constructor name while composing
// root-owned entity/workload definitions with service-owned definitions.
func contextTools() []ToolDefinition {
	tools := entityWorkloadContextTools()
	return append(tools, servicetools.ContextTools()...)
}

// serviceIntelligenceTools preserves the root package's constructor name while
// the service package owns the intelligence-report registration definition.
func serviceIntelligenceTools() []ToolDefinition {
	return servicetools.IntelligenceTools()
}

// semanticEvidenceTools preserves the root package's constructor name while
// the semantic package owns the evidence registration definitions.
func semanticEvidenceTools() []ToolDefinition {
	return semantictools.EvidenceTools()
}

// semanticSearchTools preserves the root package's constructor name while the
// semantic package owns the search registration definition.
func semanticSearchTools() []ToolDefinition {
	return semantictools.SearchTools()
}

// askTools preserves the root package's constructor name while the ask
// package owns the registration definition.
func askTools() []ToolDefinition {
	return asktools.Tools()
}

// relationshipEdgesTool preserves the root package's constructor name while
// the relationships package owns the registration definition.
func relationshipEdgesTool() ToolDefinition {
	return relationshiptools.Tool()
}
