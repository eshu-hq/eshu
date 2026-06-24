// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appsync

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS AppSync metadata facts for one claimed account and region.
// It never reads or persists the schema SDL body, resolver request/response
// mapping templates, pipeline function code bodies, or API key values, and it
// never mutates AppSync resources.
type Scanner struct {
	Client Client
}

// Scan observes GraphQL API, data source, resolver, function, schema, and API
// key metadata through the configured client and emits resource and
// relationship facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("appsync scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAppSync:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAppSync
	default:
		return nil, fmt.Errorf("appsync scanner received service_kind %q", boundary.ServiceKind)
	}
	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot AppSync metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, api := range snapshot.APIs {
		if err := appendAPI(&envelopes, boundary, api); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendAPI(envelopes *[]facts.Envelope, boundary awscloud.Boundary, api GraphQLAPI) error {
	apiID := strings.TrimSpace(api.ID)
	if apiID == "" {
		return nil
	}
	if err := appendResource(envelopes, apiObservation(boundary, api)); err != nil {
		return err
	}
	for _, relationship := range apiAuthRelationships(boundary, api) {
		if err := appendRelationship(envelopes, relationship); err != nil {
			return err
		}
	}
	for _, dataSource := range api.DataSources {
		if err := appendResource(envelopes, dataSourceObservation(boundary, api, dataSource)); err != nil {
			return err
		}
		for _, relationship := range dataSourceRelationships(boundary, api, dataSource) {
			if err := appendRelationship(envelopes, relationship); err != nil {
				return err
			}
		}
	}
	for _, resolver := range api.Resolvers {
		if err := appendResource(envelopes, resolverObservation(boundary, api, resolver)); err != nil {
			return err
		}
		if relationship := resolverDataSourceRelationship(boundary, api, resolver); relationship != nil {
			if err := appendRelationship(envelopes, *relationship); err != nil {
				return err
			}
		}
	}
	for _, function := range api.Functions {
		if err := appendResource(envelopes, functionObservation(boundary, api, function)); err != nil {
			return err
		}
		if relationship := functionDataSourceRelationship(boundary, api, function); relationship != nil {
			if err := appendRelationship(envelopes, *relationship); err != nil {
				return err
			}
		}
	}
	if api.Schema != nil {
		if err := appendResource(envelopes, schemaObservation(boundary, api, *api.Schema)); err != nil {
			return err
		}
	}
	for _, key := range api.APIKeys {
		if err := appendResource(envelopes, apiKeyObservation(boundary, api, key)); err != nil {
			return err
		}
	}
	return nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func appendResource(envelopes *[]facts.Envelope, observation awscloud.ResourceObservation) error {
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationship(envelopes *[]facts.Envelope, observation awscloud.RelationshipObservation) error {
	envelope, err := awscloud.NewRelationshipEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func apiObservation(boundary awscloud.Boundary, api GraphQLAPI) awscloud.ResourceObservation {
	apiID := strings.TrimSpace(api.ID)
	apiARN := strings.TrimSpace(api.ARN)
	attributes := map[string]any{
		"api_id":              apiID,
		"authentication_type": strings.TrimSpace(api.AuthenticationType),
		"xray_enabled":        api.XrayEnabled,
		"api_type":            strings.TrimSpace(api.APIType),
		"visibility":          strings.TrimSpace(api.Visibility),
		"waf_web_acl_arn":     strings.TrimSpace(api.WAFWebACLARN),
		"oidc_issuers":        cloneStrings(api.OIDCIssuers),
		"user_pool_ids":       userPoolIDs(api.UserPools),
	}
	if api.LogConfig != nil {
		attributes["log_config"] = map[string]any{
			"field_log_level":          strings.TrimSpace(api.LogConfig.FieldLogLevel),
			"cloudwatch_logs_role_arn": strings.TrimSpace(api.LogConfig.CloudWatchLogsRoleARN),
			"exclude_verbose_content":  api.LogConfig.ExcludeVerboseContent,
		}
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                apiARN,
		ResourceID:         apiID,
		ResourceType:       awscloud.ResourceTypeAppSyncGraphQLAPI,
		Name:               firstNonEmpty(strings.TrimSpace(api.Name), apiID),
		Tags:               cloneStringMap(api.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{apiID, apiARN, strings.TrimSpace(api.Name)},
		SourceRecordID:     apiID,
	}
}

func dataSourceObservation(boundary awscloud.Boundary, api GraphQLAPI, ds DataSource) awscloud.ResourceObservation {
	resourceID := dataSourceResourceID(api.ID, ds.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(ds.ARN),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppSyncDataSource,
		Name:         strings.TrimSpace(ds.Name),
		Attributes: map[string]any{
			"api_id":           strings.TrimSpace(api.ID),
			"data_source_name": strings.TrimSpace(ds.Name),
			"type":             strings.TrimSpace(ds.Type),
			"service_role_arn": strings.TrimSpace(ds.ServiceRoleARN),
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(ds.ARN)},
		SourceRecordID:     resourceID,
	}
}

func resolverObservation(boundary awscloud.Boundary, api GraphQLAPI, resolver Resolver) awscloud.ResourceObservation {
	resourceID := resolverResourceID(api.ID, resolver.TypeName, resolver.FieldName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(resolver.ARN),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppSyncResolver,
		Name:         resolverName(resolver),
		Attributes: map[string]any{
			"api_id":                strings.TrimSpace(api.ID),
			"type_name":             strings.TrimSpace(resolver.TypeName),
			"field_name":            strings.TrimSpace(resolver.FieldName),
			"kind":                  strings.TrimSpace(resolver.Kind),
			"data_source_name":      strings.TrimSpace(resolver.DataSourceName),
			"runtime_name":          strings.TrimSpace(resolver.RuntimeName),
			"runtime_version":       strings.TrimSpace(resolver.RuntimeVersion),
			"pipeline_function_ids": cloneStrings(resolver.PipelineFunctionIDs),
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(resolver.ARN)},
		SourceRecordID:     resourceID,
	}
}

func functionObservation(boundary awscloud.Boundary, api GraphQLAPI, function Function) awscloud.ResourceObservation {
	resourceID := functionResourceID(api.ID, function.ID, function.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(function.ARN),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppSyncFunction,
		Name:         firstNonEmpty(strings.TrimSpace(function.Name), strings.TrimSpace(function.ID)),
		Attributes: map[string]any{
			"api_id":           strings.TrimSpace(api.ID),
			"function_id":      strings.TrimSpace(function.ID),
			"data_source_name": strings.TrimSpace(function.DataSourceName),
			"runtime_name":     strings.TrimSpace(function.RuntimeName),
			"runtime_version":  strings.TrimSpace(function.RuntimeVersion),
			"function_version": strings.TrimSpace(function.FunctionVersion),
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(function.ARN)},
		SourceRecordID:     resourceID,
	}
}

func schemaObservation(boundary awscloud.Boundary, api GraphQLAPI, schema SchemaMetadata) awscloud.ResourceObservation {
	resourceID := schemaResourceID(api.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppSyncSchema,
		Name:         resourceID,
		State:        strings.TrimSpace(schema.Status),
		Attributes: map[string]any{
			"api_id":     strings.TrimSpace(api.ID),
			"status":     strings.TrimSpace(schema.Status),
			"type_count": schema.TypeCount,
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(api.ID)},
		SourceRecordID:     resourceID,
	}
}

func apiKeyObservation(boundary awscloud.Boundary, api GraphQLAPI, key APIKey) awscloud.ResourceObservation {
	resourceID := apiKeyResourceID(api.ID, key.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppSyncAPIKey,
		Name:         strings.TrimSpace(key.ID),
		Attributes: map[string]any{
			"api_id":      strings.TrimSpace(api.ID),
			"key_id":      strings.TrimSpace(key.ID),
			"description": strings.TrimSpace(key.Description),
			"expires":     timeOrNil(key.Expires),
			"deletes":     timeOrNil(key.Deletes),
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(key.ID)},
		SourceRecordID:     resourceID,
	}
}

func userPoolIDs(refs []UserPoolRef) []string {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id := strings.TrimSpace(ref.UserPoolID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}
