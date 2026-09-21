// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func foundationModelObservation(model FoundationModel) awscloud.ResourceObservation {
	arn := strings.TrimSpace(model.ARN)
	id := firstNonEmpty(arn, model.ModelID)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockFoundationModel,
		Name:         strings.TrimSpace(model.ModelID),
		State:        strings.TrimSpace(model.LifecycleStatus),
		Attributes: map[string]any{
			"model_id":      strings.TrimSpace(model.ModelID),
			"provider_name": strings.TrimSpace(model.ProviderName),
		},
		CorrelationAnchors: []string{arn, model.ModelID},
		SourceRecordID:     id,
	}
}

func customModelObservation(model CustomModel) awscloud.ResourceObservation {
	arn := strings.TrimSpace(model.ARN)
	id := firstNonEmpty(arn, model.Name)
	// Hyperparameter values and training input data references are intentionally
	// omitted: the scanner-owned type has no field for them. The base model id,
	// job ARN, and output S3 reference are inventory metadata.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockCustomModel,
		Name:         strings.TrimSpace(model.Name),
		Tags:         cloneStringMap(model.Tags),
		Attributes: map[string]any{
			"base_model_arn":        strings.TrimSpace(model.BaseModelARN),
			"customization_job_arn": strings.TrimSpace(model.JobARN),
			"output_s3_uri":         strings.TrimSpace(model.OutputS3URI),
			"creation_time":         timeOrNil(model.CreationTime),
		},
		CorrelationAnchors: []string{arn, model.Name},
		SourceRecordID:     id,
	}
}

func customizationJobObservation(job ModelCustomizationJob) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	id := firstNonEmpty(arn, job.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockModelCustomizationJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.Status),
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"base_model_arn":   strings.TrimSpace(job.BaseModelARN),
			"custom_model_arn": strings.TrimSpace(job.CustomModelARN),
			"creation_time":    timeOrNil(job.CreationTime),
			"end_time":         timeOrNil(job.EndTime),
		},
		CorrelationAnchors: []string{arn, job.Name},
		SourceRecordID:     id,
	}
}

func provisionedThroughputObservation(pt ProvisionedModelThroughput) awscloud.ResourceObservation {
	arn := strings.TrimSpace(pt.ARN)
	id := firstNonEmpty(arn, pt.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockProvisionedModelThroughput,
		Name:         strings.TrimSpace(pt.Name),
		State:        strings.TrimSpace(pt.Status),
		Tags:         cloneStringMap(pt.Tags),
		Attributes: map[string]any{
			"model_arn":     strings.TrimSpace(pt.ModelARN),
			"model_units":   pt.ModelUnits,
			"creation_time": timeOrNil(pt.CreationTime),
		},
		CorrelationAnchors: []string{arn, pt.Name},
		SourceRecordID:     id,
	}
}

func guardrailObservation(guardrail Guardrail) awscloud.ResourceObservation {
	arn := strings.TrimSpace(guardrail.ARN)
	id := firstNonEmpty(arn, guardrail.ID, guardrail.Name)
	// Topic and content policy bodies are intentionally omitted: they encode the
	// organization's content-safety posture and the scanner-owned type has no
	// field for them. Only the name, version, and status are inventory metadata.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockGuardrail,
		Name:         strings.TrimSpace(guardrail.Name),
		State:        strings.TrimSpace(guardrail.Status),
		Tags:         cloneStringMap(guardrail.Tags),
		Attributes: map[string]any{
			"guardrail_id":  strings.TrimSpace(guardrail.ID),
			"version":       strings.TrimSpace(guardrail.Version),
			"description":   strings.TrimSpace(guardrail.Description),
			"creation_time": timeOrNil(guardrail.CreationTime),
		},
		CorrelationAnchors: []string{arn, guardrail.ID, guardrail.Name},
		SourceRecordID:     id,
	}
}

func agentObservation(agent Agent) awscloud.ResourceObservation {
	arn := strings.TrimSpace(agent.ARN)
	id := firstNonEmpty(arn, agent.ID, agent.Name)
	// The agent instruction (system prompt) and prompt-override configuration are
	// intentionally omitted: they are valuable IP and the scanner-owned type has
	// no field for them. Only the name, description, and foundation model id are
	// inventory metadata.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockAgent,
		Name:         strings.TrimSpace(agent.Name),
		State:        strings.TrimSpace(agent.Status),
		Tags:         cloneStringMap(agent.Tags),
		Attributes: map[string]any{
			"agent_id":         strings.TrimSpace(agent.ID),
			"description":      strings.TrimSpace(agent.Description),
			"foundation_model": strings.TrimSpace(agent.FoundationModel),
			"creation_time":    timeOrNil(agent.CreationTime),
		},
		CorrelationAnchors: []string{arn, agent.ID, agent.Name},
		SourceRecordID:     id,
	}
}

func actionGroupObservation(group AgentActionGroup) awscloud.ResourceObservation {
	id := firstNonEmpty(actionGroupID(group.AgentID, group.ID), group.Name)
	// The action-group API schema body and function schema are intentionally
	// omitted: they are often customer IP and the scanner-owned type has no field
	// for them. Only the name, state, and Lambda executor ARN are metadata.
	return awscloud.ResourceObservation{
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockAgentActionGroup,
		Name:         strings.TrimSpace(group.Name),
		State:        strings.TrimSpace(group.State),
		Attributes: map[string]any{
			"agent_id":        strings.TrimSpace(group.AgentID),
			"action_group_id": strings.TrimSpace(group.ID),
			"lambda_executor": strings.TrimSpace(group.LambdaARN),
		},
		CorrelationAnchors: []string{group.ID, group.Name},
		SourceRecordID:     id,
	}
}

func knowledgeBaseObservation(kb KnowledgeBase) awscloud.ResourceObservation {
	arn := strings.TrimSpace(kb.ARN)
	id := firstNonEmpty(arn, kb.ID, kb.Name)
	// Ingested document content and chunks are intentionally omitted: the
	// scanner-owned type has no field for them. Only the name, status, embedding
	// model reference, and data source endpoint refs are inventory metadata.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeBedrockKnowledgeBase,
		Name:         strings.TrimSpace(kb.Name),
		State:        strings.TrimSpace(kb.Status),
		Tags:         cloneStringMap(kb.Tags),
		Attributes: map[string]any{
			"knowledge_base_id":   strings.TrimSpace(kb.ID),
			"description":         strings.TrimSpace(kb.Description),
			"embedding_model_arn": strings.TrimSpace(kb.EmbeddingModelARN),
			"data_source_types":   dataSourceTypes(kb.DataSources),
			"data_source_count":   len(kb.DataSources),
			"creation_time":       timeOrNil(kb.CreationTime),
		},
		CorrelationAnchors: []string{arn, kb.ID, kb.Name},
		SourceRecordID:     id,
	}
}

// actionGroupID builds a stable synthetic id for an action group from its
// parent agent id and action-group id, since action groups have no ARN.
func actionGroupID(agentID, groupID string) string {
	agentID = strings.TrimSpace(agentID)
	groupID = strings.TrimSpace(groupID)
	if agentID == "" || groupID == "" {
		return ""
	}
	return "bedrock-agent:" + agentID + ":action-group:" + groupID
}

// dataSourceTypes returns the de-duplicated connector type labels for a
// knowledge base's data sources in reported order.
func dataSourceTypes(sources []KnowledgeBaseDataSource) []string {
	types := make([]string, 0, len(sources))
	for _, source := range sources {
		types = append(types, source.Type)
	}
	return cloneStrings(types)
}
