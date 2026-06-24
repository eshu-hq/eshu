// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	cptypes "github.com/aws/aws-sdk-go-v2/service/codepipeline/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cpservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codepipeline"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// sourceRevisionSummaryReason labels redacted source-revision summaries so
// downstream audit can trace why the value was replaced. Commit-message and
// user-provided summaries may carry developer-pasted secrets.
const sourceRevisionSummaryReason = "codepipeline_source_revision_summary"

// targetConfigKeys maps a resolved target provider class to the ordered list of
// non-secret action configuration keys that name the concrete target resource.
// These keys carry resource identifiers only (project, application, function,
// stack, cluster, service names), never tokens or secrets, so the adapter may
// read their values to derive a graph join while every other configuration
// value is dropped.
var targetConfigKeys = map[string][]string{
	"CodeBuild":      {"ProjectName"},
	"CodeDeploy":     {"ApplicationName"},
	"Lambda":         {"FunctionName"},
	"CloudFormation": {"StackName"},
	// ECS uses two keys; mapAction joins them as cluster/service.
	"ECS": {"ClusterName", "ServiceName"},
}

// mapPipelineDeclaration builds the scanner-owned pipeline from the GetPipeline
// declaration and its metadata. The ARN and the Created/Updated timestamps come
// from PipelineMetadata, not the declaration, so the adapter must thread the
// metadata through; otherwise created/updated stay null in emitted facts.
func mapPipelineDeclaration(decl *cptypes.PipelineDeclaration, metadata *cptypes.PipelineMetadata) cpservice.Pipeline {
	if decl == nil {
		return cpservice.Pipeline{}
	}
	pipeline := cpservice.Pipeline{
		Name:          aws.ToString(decl.Name),
		ARN:           pipelineMetadataARN(metadata),
		RoleARN:       aws.ToString(decl.RoleArn),
		PipelineType:  string(decl.PipelineType),
		ExecutionMode: string(decl.ExecutionMode),
		ArtifactStore: mapArtifactStore(decl.ArtifactStore, decl.ArtifactStores),
		Stages:        mapStages(decl.Stages),
	}
	if decl.Version != nil {
		pipeline.Version = *decl.Version
	}
	if metadata != nil {
		if metadata.Created != nil {
			pipeline.Created = metadata.Created.UTC()
		}
		if metadata.Updated != nil {
			pipeline.Updated = metadata.Updated.UTC()
		}
	}
	return pipeline
}

func mapArtifactStore(store *cptypes.ArtifactStore, stores map[string]cptypes.ArtifactStore) cpservice.ArtifactStoreSummary {
	if store == nil {
		return mapMultiRegionArtifactStores(stores)
	}
	summary := cpservice.ArtifactStoreSummary{
		Type:     string(store.Type),
		S3Bucket: strings.TrimSpace(aws.ToString(store.Location)),
	}
	if store.EncryptionKey != nil {
		summary.KMSKeyID = strings.TrimSpace(aws.ToString(store.EncryptionKey.Id))
		summary.KMSKeyType = string(store.EncryptionKey.Type)
	}
	return summary
}

// mapMultiRegionArtifactStores summarizes a cross-region artifact-store map. It
// keeps the region keys and, when present, the pipeline-region store details.
// The summary carries store identifiers only.
func mapMultiRegionArtifactStores(stores map[string]cptypes.ArtifactStore) cpservice.ArtifactStoreSummary {
	if len(stores) == 0 {
		return cpservice.ArtifactStoreSummary{}
	}
	regions := make([]string, 0, len(stores))
	for region := range stores {
		if trimmed := strings.TrimSpace(region); trimmed != "" {
			regions = append(regions, trimmed)
		}
	}
	sort.Strings(regions)
	summary := cpservice.ArtifactStoreSummary{Regions: regions}
	if len(regions) == 0 {
		return summary
	}
	// Surface the first region's store so the summary still records a bucket and
	// key when only cross-region stores are declared.
	store := stores[regions[0]]
	summary.Type = string(store.Type)
	summary.S3Bucket = strings.TrimSpace(aws.ToString(store.Location))
	if store.EncryptionKey != nil {
		summary.KMSKeyID = strings.TrimSpace(aws.ToString(store.EncryptionKey.Id))
		summary.KMSKeyType = string(store.EncryptionKey.Type)
	}
	return summary
}

func mapStages(stages []cptypes.StageDeclaration) []cpservice.Stage {
	if len(stages) == 0 {
		return nil
	}
	out := make([]cpservice.Stage, 0, len(stages))
	for _, stage := range stages {
		actions := make([]cpservice.Action, 0, len(stage.Actions))
		for _, action := range stage.Actions {
			actions = append(actions, mapAction(action))
		}
		out = append(out, cpservice.Stage{
			Name:    strings.TrimSpace(aws.ToString(stage.Name)),
			Actions: actions,
		})
	}
	return out
}

// mapAction converts one SDK action declaration into a scanner-owned action.
// It retains the sorted configuration KEY names only and never copies any
// configuration VALUE into the scanner type. For build/deploy/invoke actions it
// reads only the allowlisted non-secret target-identifier keys to resolve a
// graph-join target; every other value, including OAuthToken and SecretToken,
// is dropped.
func mapAction(decl cptypes.ActionDeclaration) cpservice.Action {
	action := cpservice.Action{
		Name:              strings.TrimSpace(aws.ToString(decl.Name)),
		Region:            strings.TrimSpace(aws.ToString(decl.Region)),
		RoleARN:           strings.TrimSpace(aws.ToString(decl.RoleArn)),
		ConfigurationKeys: configurationKeys(decl.Configuration),
	}
	if decl.RunOrder != nil {
		action.RunOrder = *decl.RunOrder
	}
	if decl.ActionTypeId != nil {
		action.Category = string(decl.ActionTypeId.Category)
		action.Owner = string(decl.ActionTypeId.Owner)
		action.Provider = strings.TrimSpace(aws.ToString(decl.ActionTypeId.Provider))
		action.Version = strings.TrimSpace(aws.ToString(decl.ActionTypeId.Version))
	}

	if strings.EqualFold(action.Category, "Source") {
		action.SourceProvider = action.Provider
		return action
	}

	if provider, ok := targetProviderForAction(action.Category, action.Provider); ok {
		action.TargetProvider = provider
		action.TargetResourceName = resolveTargetName(provider, decl.Configuration)
	}
	return action
}

// configurationKeys returns the sorted, deduplicated KEY names of an action
// configuration. It never returns or copies any configuration value.
func configurationKeys(config map[string]string) []string {
	if len(config) == 0 {
		return nil
	}
	keys := make([]string, 0, len(config))
	for key := range config {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys
}

// targetProviderForAction classifies a build/deploy/invoke action provider into
// a resolved target class. It returns false for providers with no known
// concrete Eshu target node.
func targetProviderForAction(category, provider string) (string, bool) {
	provider = strings.TrimSpace(provider)
	switch strings.TrimSpace(provider) {
	case "CodeBuild":
		return "CodeBuild", true
	case "CodeDeploy":
		return "CodeDeploy", true
	case "Lambda":
		return "Lambda", true
	case "CloudFormation":
		return "CloudFormation", true
	case "ECS", "ECSBlueGreen", "ECSDeploy":
		return "ECS", true
	default:
		return "", false
	}
}

// resolveTargetName reads only the allowlisted non-secret identifier keys for a
// target provider and returns the concrete target name. For ECS it returns
// "cluster/service". It returns "" when the identifier keys are absent. It
// never reads any non-allowlisted configuration value.
func resolveTargetName(provider string, config map[string]string) string {
	keys := targetConfigKeys[provider]
	if len(keys) == 0 || len(config) == 0 {
		return ""
	}
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(config[key])
		if value == "" {
			return ""
		}
		values = append(values, value)
	}
	return strings.Join(values, "/")
}

func mapExecution(pipelineName string, summary cptypes.PipelineExecutionSummary, key redact.Key) cpservice.Execution {
	execution := cpservice.Execution{
		PipelineName:    strings.TrimSpace(pipelineName),
		ID:              strings.TrimSpace(aws.ToString(summary.PipelineExecutionId)),
		Status:          string(summary.Status),
		ExecutionMode:   string(summary.ExecutionMode),
		ExecutionType:   string(summary.ExecutionType),
		SourceRevisions: mapSourceRevisions(summary.SourceRevisions, key),
	}
	if summary.StartTime != nil {
		execution.StartTime = summary.StartTime.UTC()
	}
	if summary.LastUpdateTime != nil {
		execution.LastUpdateTime = summary.LastUpdateTime.UTC()
	}
	if summary.Trigger != nil {
		execution.TriggerType = string(summary.Trigger.TriggerType)
	}
	return execution
}

// mapSourceRevisions keeps safe revision references and redacts the
// commit-message summary. RevisionSummary may carry developer-pasted secrets,
// so it is routed through the redaction library and never copied raw.
func mapSourceRevisions(revisions []cptypes.SourceRevision, key redact.Key) []cpservice.SourceRevision {
	if len(revisions) == 0 {
		return nil
	}
	out := make([]cpservice.SourceRevision, 0, len(revisions))
	for _, revision := range revisions {
		summary := strings.TrimSpace(aws.ToString(revision.RevisionSummary))
		mapped := cpservice.SourceRevision{
			ActionName:  strings.TrimSpace(aws.ToString(revision.ActionName)),
			RevisionID:  strings.TrimSpace(aws.ToString(revision.RevisionId)),
			RevisionURL: strings.TrimSpace(aws.ToString(revision.RevisionUrl)),
			HasSummary:  summary != "",
		}
		if mapped.HasSummary {
			mapped.SummaryMarker = awscloud.RedactString(summary, sourceRevisionSummaryReason, key)
		}
		out = append(out, mapped)
	}
	return out
}

// mapWebhook keeps webhook metadata only. The authentication secret token in
// AuthenticationConfiguration is never read into the scanner type.
func mapWebhook(item cptypes.ListWebhookItem) cpservice.Webhook {
	webhook := cpservice.Webhook{
		ARN:  strings.TrimSpace(aws.ToString(item.Arn)),
		Tags: mapTags(item.Tags),
	}
	if item.LastTriggered != nil {
		webhook.LastTriggered = item.LastTriggered.UTC()
	}
	if item.Definition != nil {
		webhook.Name = strings.TrimSpace(aws.ToString(item.Definition.Name))
		webhook.TargetPipeline = strings.TrimSpace(aws.ToString(item.Definition.TargetPipeline))
		webhook.TargetAction = strings.TrimSpace(aws.ToString(item.Definition.TargetAction))
		webhook.AuthenticationType = string(item.Definition.Authentication)
	}
	return webhook
}

// mapActionType keeps custom action-type identity and configuration property
// KEY names only.
func mapActionType(actionType cptypes.ActionType) cpservice.ActionType {
	mapped := cpservice.ActionType{}
	if actionType.Id != nil {
		mapped.Category = string(actionType.Id.Category)
		mapped.Owner = string(actionType.Id.Owner)
		mapped.Provider = strings.TrimSpace(aws.ToString(actionType.Id.Provider))
		mapped.Version = strings.TrimSpace(aws.ToString(actionType.Id.Version))
	}
	if len(actionType.ActionConfigurationProperties) > 0 {
		names := make([]string, 0, len(actionType.ActionConfigurationProperties))
		for _, property := range actionType.ActionConfigurationProperties {
			if name := strings.TrimSpace(aws.ToString(property.Name)); name != "" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		mapped.ConfigurationPropertyKeys = names
	}
	return mapped
}

func mapTags(tags []cptypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
