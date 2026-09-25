// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// foundationModelTargetType labels a base or provisioned model ARN relationship
// target. Bedrock base and foundation model ARNs are managed by AWS and are not
// emitted as scanner resources of their own, so the target is the foundation
// model resource type the scanner does emit for the read-only model list.
const foundationModelTargetType = aws.ResourceTypeBedrockFoundationModel

func customModelRelationships(model CustomModel) []aws.RelationshipObservation {
	id := firstNonEmpty(model.ARN, model.Name)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if base := strings.TrimSpace(model.BaseModelARN); isARN(base) {
		observations = append(observations, aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockCustomModelUsesBaseModel,
			SourceResourceID: id,
			SourceARN:        strings.TrimSpace(model.ARN),
			TargetResourceID: base,
			TargetARN:        base,
			TargetType:       foundationModelTargetType,
			SourceRecordID:   id + "#base-model#" + base,
		})
	}
	if job := strings.TrimSpace(model.JobARN); isARN(job) {
		observations = append(observations, aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockCustomModelFromCustomizationJob,
			SourceResourceID: id,
			SourceARN:        strings.TrimSpace(model.ARN),
			TargetResourceID: job,
			TargetARN:        job,
			TargetType:       aws.ResourceTypeBedrockModelCustomizationJob,
			SourceRecordID:   id + "#job#" + job,
		})
	}
	if observation, ok := customModelS3Relationship(id, model.ARN, model.OutputS3URI); ok {
		observations = append(observations, observation)
	}
	return observations
}

// customModelS3Relationship maps an `s3://bucket/key` output URI to an S3 bucket
// relationship target, taking the partition from the custom model ARN so the
// synthesized bucket ARN is correct in any AWS partition. It returns ok=false
// for blank, non-S3 URIs.
func customModelS3Relationship(modelID, modelARN, outputURI string) (aws.RelationshipObservation, bool) {
	uri := strings.TrimSpace(outputURI)
	if uri == "" {
		return aws.RelationshipObservation{}, false
	}
	bucket, key, ok := parseS3URL(uri)
	if !ok {
		return aws.RelationshipObservation{}, false
	}
	bucketARN := s3BucketARN(aws.PartitionFromARN(modelARN), bucket)
	attributes := map[string]any{"output_s3_uri": uri, "bucket": bucket}
	if key != "" {
		attributes["object_key"] = key
	}
	return aws.RelationshipObservation{
		RelationshipType: aws.RelationshipBedrockCustomModelUsesS3Output,
		SourceResourceID: modelID,
		SourceARN:        strings.TrimSpace(modelARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   modelID + "#output#" + uri,
	}, true
}

func provisionedThroughputRelationships(pt ProvisionedModelThroughput) []aws.RelationshipObservation {
	id := firstNonEmpty(pt.ARN, pt.Name)
	model := strings.TrimSpace(pt.ModelARN)
	if id == "" || !isARN(model) {
		return nil
	}
	return []aws.RelationshipObservation{{
		RelationshipType: aws.RelationshipBedrockProvisionedThroughputUsesModel,
		SourceResourceID: id,
		SourceARN:        strings.TrimSpace(pt.ARN),
		TargetResourceID: model,
		TargetARN:        model,
		TargetType:       foundationModelTargetType,
		SourceRecordID:   id + "#model#" + model,
	}}
}

func agentRelationships(agent Agent) []aws.RelationshipObservation {
	id := firstNonEmpty(agent.ARN, agent.ID, agent.Name)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if model := strings.TrimSpace(agent.FoundationModel); model != "" {
		observations = append(observations, aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockAgentUsesFoundationModel,
			SourceResourceID: id,
			SourceARN:        strings.TrimSpace(agent.ARN),
			TargetResourceID: model,
			TargetType:       foundationModelTargetType,
			SourceRecordID:   id + "#foundation-model#" + model,
		})
	}
	seen := make(map[string]struct{}, len(agent.KnowledgeBaseIDs))
	for _, kbID := range agent.KnowledgeBaseIDs {
		kbID = strings.TrimSpace(kbID)
		if kbID == "" {
			continue
		}
		if _, ok := seen[kbID]; ok {
			continue
		}
		seen[kbID] = struct{}{}
		observations = append(observations, aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockAgentUsesKnowledgeBase,
			SourceResourceID: id,
			SourceARN:        strings.TrimSpace(agent.ARN),
			TargetResourceID: kbID,
			TargetType:       aws.ResourceTypeBedrockKnowledgeBase,
			SourceRecordID:   id + "#knowledge-base#" + kbID,
		})
	}
	return observations
}

func actionGroupRelationships(group AgentActionGroup) []aws.RelationshipObservation {
	id := firstNonEmpty(actionGroupID(group.AgentID, group.ID), group.Name)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if agentID := strings.TrimSpace(group.AgentID); agentID != "" {
		observations = append(observations, aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockAgentHasActionGroup,
			SourceResourceID: agentID,
			TargetResourceID: id,
			TargetType:       aws.ResourceTypeBedrockAgentActionGroup,
			SourceRecordID:   agentID + "#action-group#" + id,
		})
	}
	if lambda := strings.TrimSpace(group.LambdaARN); isARN(lambda) {
		observations = append(observations, aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockActionGroupUsesLambda,
			SourceResourceID: id,
			TargetResourceID: lambda,
			TargetARN:        lambda,
			TargetType:       aws.ResourceTypeLambdaFunction,
			SourceRecordID:   id + "#lambda#" + lambda,
		})
	}
	return observations
}

func knowledgeBaseRelationships(kb KnowledgeBase) []aws.RelationshipObservation {
	id := firstNonEmpty(kb.ARN, kb.ID, kb.Name)
	if id == "" {
		return nil
	}
	partition := aws.PartitionFromARN(kb.ARN)
	var observations []aws.RelationshipObservation
	for _, source := range kb.DataSources {
		if observation, ok := knowledgeBaseDataSourceRelationship(id, kb.ARN, partition, source); ok {
			observations = append(observations, observation)
		}
	}
	return observations
}

// knowledgeBaseDataSourceRelationship maps one knowledge base data source to a
// typed relationship target. S3 sources resolve to an S3 bucket ARN; Confluence,
// SharePoint, and web-crawler sources carry their host/site/seed URL as the
// target reference. It returns ok=false for unaddressable sources.
func knowledgeBaseDataSourceRelationship(
	kbID, kbARN, partition string,
	source KnowledgeBaseDataSource,
) (aws.RelationshipObservation, bool) {
	attributes := map[string]any{"data_source_type": strings.TrimSpace(source.Type)}
	if name := strings.TrimSpace(source.Name); name != "" {
		attributes["data_source_name"] = name
	}
	if dsID := strings.TrimSpace(source.ID); dsID != "" {
		attributes["data_source_id"] = dsID
	}
	if bucketArg := strings.TrimSpace(source.S3BucketARN); bucketArg != "" {
		bucketARN, ok := normalizeS3BucketARN(partition, bucketArg)
		if !ok {
			// A reported S3 reference that resolves to an empty bucket name (for
			// example an `s3://` or `s3:///path` scheme prefix) cannot address a
			// real bucket. Dropping the edge keeps an invalid empty join key out
			// of the graph rather than emitting `arn:<partition>:s3:::`.
			return aws.RelationshipObservation{}, false
		}
		return aws.RelationshipObservation{
			RelationshipType: aws.RelationshipBedrockKnowledgeBaseUsesS3DataSource,
			SourceResourceID: kbID,
			SourceARN:        strings.TrimSpace(kbARN),
			TargetResourceID: bucketARN,
			TargetARN:        bucketARN,
			TargetType:       aws.ResourceTypeS3Bucket,
			Attributes:       attributes,
			SourceRecordID:   kbID + "#s3-data-source#" + bucketARN,
		}, true
	}
	url := strings.TrimSpace(source.URL)
	if url == "" {
		return aws.RelationshipObservation{}, false
	}
	relationshipType, targetType, ok := urlDataSourceKinds(source.Type)
	if !ok {
		return aws.RelationshipObservation{}, false
	}
	attributes["url"] = url
	return aws.RelationshipObservation{
		RelationshipType: relationshipType,
		SourceResourceID: kbID,
		SourceARN:        strings.TrimSpace(kbARN),
		TargetResourceID: url,
		TargetType:       targetType,
		Attributes:       attributes,
		SourceRecordID:   kbID + "#" + targetType + "#" + url,
	}, true
}

// urlDataSourceKinds maps a connector type to its relationship and external
// target type for URL-addressed sources. It returns ok=false for connector
// types that are not URL-addressed.
func urlDataSourceKinds(dataSourceType string) (relationshipType, targetType string, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(dataSourceType)) {
	case "CONFLUENCE":
		return aws.RelationshipBedrockKnowledgeBaseUsesConfluence, "confluence_data_source", true
	case "SHAREPOINT":
		return aws.RelationshipBedrockKnowledgeBaseUsesSharePoint, "sharepoint_data_source", true
	case "WEB":
		return aws.RelationshipBedrockKnowledgeBaseUsesWebCrawler, "web_data_source", true
	default:
		return "", "", false
	}
}

// normalizeS3BucketARN resolves an S3 data-source reference to a bucket ARN,
// reporting ok=false when no bucket can be determined. It returns bucketArg
// unchanged when it is already an S3 bucket ARN, otherwise it synthesizes one
// from the partition and a bare bucket name. Bedrock reports a BucketArn for S3
// data sources, so the ARN branch is the common case; the name branch guards
// against any bare-name or `s3://` URL input. A non-ARN input that yields an
// empty bucket after trimming the scheme and path (for example `s3://` or
// `s3:///path`) is unresolvable, so it returns ""/ok=false instead of
// synthesizing an invalid `arn:<partition>:s3:::` with an empty join key.
func normalizeS3BucketARN(partition, bucketArg string) (string, bool) {
	if strings.HasPrefix(bucketArg, "arn:") {
		return bucketArg, true
	}
	bucket := strings.TrimPrefix(bucketArg, "s3://")
	bucket, _, _ = strings.Cut(bucket, "/")
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return "", false
	}
	return s3BucketARN(partition, bucket), true
}
