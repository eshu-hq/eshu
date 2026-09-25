// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package glue

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Glue metadata-only facts for one claimed account and
// region. It never runs jobs, starts crawlers, mutates Data Catalog state, or
// reads job script bodies, default-argument values, connection passwords, or
// table column statistics that contain sample values.
type Scanner struct {
	Client Client
}

// Scan observes Glue Data Catalog databases and tables, crawlers, jobs,
// triggers, workflows, and connections through the configured client. Job
// script bodies, default-argument values, connection passwords, JDBC
// credential URLs, table column sample statistics, and classifier custom
// patterns stay outside the scanner contract.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("glue scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceGlue:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceGlue
	default:
		return nil, fmt.Errorf("glue scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	databases, err := s.Client.ListDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Glue databases: %w", err)
	}
	for _, database := range databases {
		next, err := databaseEnvelopes(boundary, database)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	crawlers, err := s.Client.ListCrawlers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Glue crawlers: %w", err)
	}
	for _, crawler := range crawlers {
		next, err := crawlerEnvelopes(boundary, crawler)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	jobs, err := s.Client.ListJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Glue jobs: %w", err)
	}
	for _, job := range jobs {
		next, err := jobEnvelopes(boundary, job)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	triggers, err := s.Client.ListTriggers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Glue triggers: %w", err)
	}
	for _, trigger := range triggers {
		next, err := triggerEnvelopes(boundary, trigger)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	workflows, err := s.Client.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Glue workflows: %w", err)
	}
	for _, workflow := range workflows {
		envelope, err := aws.NewResourceEnvelope(workflowObservation(boundary, workflow))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	connections, err := s.Client.ListConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Glue connections: %w", err)
	}
	for _, connection := range connections {
		envelope, err := aws.NewResourceEnvelope(connectionObservation(boundary, connection))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func databaseEnvelopes(boundary aws.Boundary, database Database) ([]facts.Envelope, error) {
	databaseResource, err := aws.NewResourceEnvelope(databaseObservation(boundary, database))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{databaseResource}
	for _, table := range database.Tables {
		if strings.TrimSpace(table.DatabaseName) == "" {
			table.DatabaseName = database.Name
		}
		tableResource, err := aws.NewResourceEnvelope(tableObservation(boundary, table))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, tableResource)
		for _, relationship := range []*aws.RelationshipObservation{
			tableInDatabaseRelationship(boundary, table),
			tableS3LocationRelationship(boundary, table),
		} {
			if relationship == nil {
				continue
			}
			envelope, err := aws.NewRelationshipEnvelope(*relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func crawlerEnvelopes(boundary aws.Boundary, crawler Crawler) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(crawlerObservation(boundary, crawler))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*aws.RelationshipObservation{
		crawlerDatabaseRelationship(boundary, crawler),
		crawlerRoleRelationship(boundary, crawler),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func jobEnvelopes(boundary aws.Boundary, job Job) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(jobObservation(boundary, job))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := jobRoleRelationship(boundary, job); relationship != nil {
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func triggerEnvelopes(boundary aws.Boundary, trigger Trigger) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(triggerObservation(boundary, trigger))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range triggerJobRelationships(boundary, trigger) {
		envelope, err := aws.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func databaseObservation(boundary aws.Boundary, database Database) aws.ResourceObservation {
	name := strings.TrimSpace(database.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeGlueDatabase,
		Name:         name,
		Attributes: map[string]any{
			"catalog_id":   strings.TrimSpace(database.CatalogID),
			"description":  strings.TrimSpace(database.Description),
			"location_uri": strings.TrimSpace(database.LocationURI),
			"create_time":  timeOrNil(database.CreateTime),
			"parameters":   cloneStringMap(database.Parameters),
			"table_count":  len(database.Tables),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func tableObservation(boundary aws.Boundary, table Table) aws.ResourceObservation {
	tableID := tableResourceID(table)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   tableID,
		ResourceType: aws.ResourceTypeGlueTable,
		Name:         strings.TrimSpace(table.Name),
		Attributes: map[string]any{
			"catalog_id":         strings.TrimSpace(table.CatalogID),
			"database_name":      strings.TrimSpace(table.DatabaseName),
			"owner":              strings.TrimSpace(table.Owner),
			"table_type":         strings.TrimSpace(table.TableType),
			"description":        strings.TrimSpace(table.Description),
			"create_time":        timeOrNil(table.CreateTime),
			"update_time":        timeOrNil(table.UpdateTime),
			"last_access_time":   timeOrNil(table.LastAccessTime),
			"last_analyzed_time": timeOrNil(table.LastAnalyzedTime),
			"retention":          table.Retention,
			"storage_location":   strings.TrimSpace(table.StorageLocation),
			"input_format":       strings.TrimSpace(table.InputFormat),
			"output_format":      strings.TrimSpace(table.OutputFormat),
			"compressed":         table.Compressed,
			"serde_name":         strings.TrimSpace(table.SerdeName),
			"serde_library":      strings.TrimSpace(table.SerdeLibrary),
			"parameters":         cloneStringMap(table.Parameters),
			"partition_keys":     cloneStringSlice(table.PartitionKeys),
			"columns":            cloneStringSlice(table.Columns),
		},
		CorrelationAnchors: []string{tableID, strings.TrimSpace(table.Name)},
		SourceRecordID:     tableID,
	}
}

func crawlerObservation(boundary aws.Boundary, crawler Crawler) aws.ResourceObservation {
	name := strings.TrimSpace(crawler.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeGlueCrawler,
		Name:         name,
		State:        strings.TrimSpace(crawler.State),
		Attributes: map[string]any{
			"description":           strings.TrimSpace(crawler.Description),
			"role_arn":              strings.TrimSpace(crawler.RoleARN),
			"database_name":         strings.TrimSpace(crawler.DatabaseName),
			"table_prefix":          strings.TrimSpace(crawler.TablePrefix),
			"creation_time":         timeOrNil(crawler.CreationTime),
			"last_updated":          timeOrNil(crawler.LastUpdated),
			"schedule":              strings.TrimSpace(crawler.Schedule),
			"recrawl_behavior":      strings.TrimSpace(crawler.RecrawlBehavior),
			"s3_target_count":       crawler.S3TargetCount,
			"jdbc_target_count":     crawler.JDBCTargetCount,
			"dynamodb_target_count": crawler.DynamoDBTargetCount,
			"catalog_target_count":  crawler.CatalogTargetCount,
			"mongodb_target_count":  crawler.MongoDBTargetCount,
			"delta_target_count":    crawler.DeltaTargetCount,
			"iceberg_target_count":  crawler.IcebergTargetCount,
			"hudi_target_count":     crawler.HudiTargetCount,
			"configuration_version": strings.TrimSpace(crawler.ConfigurationVersion),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func jobObservation(boundary aws.Boundary, job Job) aws.ResourceObservation {
	name := strings.TrimSpace(job.Name)
	safeDefaultArgKeys := filterSafeKeys(job.DefaultArgKeys)
	safeNonOverridableKeys := filterSafeKeys(job.NonOverridableArgKeys)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeGlueJob,
		Name:         name,
		Attributes: map[string]any{
			"description":                   strings.TrimSpace(job.Description),
			"role_arn":                      strings.TrimSpace(job.RoleARN),
			"glue_version":                  strings.TrimSpace(job.GlueVersion),
			"worker_type":                   strings.TrimSpace(job.WorkerType),
			"number_of_workers":             job.NumberOfWorkers,
			"max_capacity":                  job.MaxCapacity,
			"max_retries":                   job.MaxRetries,
			"timeout":                       job.Timeout,
			"script_language":               strings.TrimSpace(job.ScriptLanguage),
			"script_location":               strings.TrimSpace(job.ScriptLocation),
			"command_name":                  strings.TrimSpace(job.CommandName),
			"created_on":                    timeOrNil(job.CreatedOn),
			"last_modified_on":              timeOrNil(job.LastModifiedOn),
			"security_configuration":        strings.TrimSpace(job.SecurityConfiguration),
			"default_argument_keys":         safeDefaultArgKeys,
			"non_overridable_argument_keys": safeNonOverridableKeys,
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func triggerObservation(boundary aws.Boundary, trigger Trigger) aws.ResourceObservation {
	name := strings.TrimSpace(trigger.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeGlueTrigger,
		Name:         name,
		State:        strings.TrimSpace(trigger.State),
		Attributes: map[string]any{
			"trigger_type":  strings.TrimSpace(trigger.Type),
			"description":   strings.TrimSpace(trigger.Description),
			"schedule":      strings.TrimSpace(trigger.Schedule),
			"workflow_name": strings.TrimSpace(trigger.WorkflowName),
			"action_jobs":   cloneStringSlice(trigger.ActionJobs),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func workflowObservation(boundary aws.Boundary, workflow Workflow) aws.ResourceObservation {
	name := strings.TrimSpace(workflow.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeGlueWorkflow,
		Name:         name,
		Attributes: map[string]any{
			"description":              strings.TrimSpace(workflow.Description),
			"created_on":               timeOrNil(workflow.CreatedOn),
			"last_modified_on":         timeOrNil(workflow.LastModifiedOn),
			"default_run_keys":         cloneStringSlice(workflow.DefaultRunKeys),
			"max_concurrent_run_count": workflow.MaxConcurrentRun,
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func connectionObservation(boundary aws.Boundary, connection Connection) aws.ResourceObservation {
	name := strings.TrimSpace(connection.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeGlueConnection,
		Name:         name,
		Attributes: map[string]any{
			"description":        strings.TrimSpace(connection.Description),
			"connection_type":    strings.TrimSpace(connection.ConnectionType),
			"creation_time":      timeOrNil(connection.CreationTime),
			"last_updated_time":  timeOrNil(connection.LastUpdatedTime),
			"last_updated_by":    strings.TrimSpace(connection.LastUpdatedBy),
			"match_criteria":     cloneStringSlice(connection.MatchCriteria),
			"availability_zone":  strings.TrimSpace(connection.PhysicalRequirementsAZ),
			"subnet_id":          strings.TrimSpace(connection.SubnetID),
			"security_group_ids": cloneStringSlice(connection.SecurityGroupIDs),
			"property_keys":      filterSafeKeys(connection.PropertyKeys),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}
