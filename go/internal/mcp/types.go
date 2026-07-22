// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// ToolDefinition describes one MCP tool exposed to clients.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ReadOnlyTools returns all read-only MCP tool definitions.
func ReadOnlyTools() []ToolDefinition {
	tools := make([]ToolDefinition, 0, 160)
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
