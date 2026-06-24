package databrew

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Glue DataBrew metadata-only facts for one claimed account
// and region. It never reads or persists recipe step expressions,
// transformation parameters, custom SQL query strings, sample data, or any
// data-plane payload, and never mutates DataBrew state. It reports datasets,
// recipes, jobs, and projects plus the dataset-to-S3, dataset-to-Glue-table,
// job-to-S3, job-to-IAM-role, job-to-dataset, project-to-dataset,
// project-to-recipe, and project-to-IAM-role relationships.
type Scanner struct {
	// Client is the metadata-only DataBrew snapshot source.
	Client Client
}

// Scan observes DataBrew datasets, recipes, jobs, and projects plus their
// direct S3, Glue Data Catalog, IAM, and internal dependency metadata through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("databrew scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDatabrew:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDatabrew
	default:
		return nil, fmt.Errorf("databrew scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DataBrew metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, dataset := range snapshot.Datasets {
		next, err := datasetEnvelopes(boundary, dataset)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, recipe := range snapshot.Recipes {
		envelope, err := awscloud.NewResourceEnvelope(recipeObservation(boundary, recipe))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, job := range snapshot.Jobs {
		next, err := jobEnvelopes(boundary, job)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, project := range snapshot.Projects {
		next, err := projectEnvelopes(boundary, project)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
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

func datasetEnvelopes(boundary awscloud.Boundary, dataset Dataset) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(datasetObservation(boundary, dataset))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	return appendRelationships(
		envelopes,
		datasetReadsS3Relationship(boundary, dataset),
		datasetReadsGlueTableRelationship(boundary, dataset),
	)
}

func jobEnvelopes(boundary awscloud.Boundary, job Job) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(jobObservation(boundary, job))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	envelopes, err = appendRelationships(
		envelopes,
		jobAssumesRoleRelationship(boundary, job),
		jobProcessesDatasetRelationship(boundary, job),
	)
	if err != nil {
		return nil, err
	}
	for _, relationship := range jobWritesS3Relationships(boundary, job) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func projectEnvelopes(boundary awscloud.Boundary, project Project) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(projectObservation(boundary, project))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	return appendRelationships(
		envelopes,
		projectUsesDatasetRelationship(boundary, project),
		projectUsesRecipeRelationship(boundary, project),
		projectAssumesRoleRelationship(boundary, project),
	)
}

// appendRelationships appends an envelope for each non-nil relationship,
// skipping the nil sentinels the relationship constructors return when an
// endpoint identity is missing.
func appendRelationships(
	envelopes []facts.Envelope,
	relationships ...*awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	for _, relationship := range relationships {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func datasetObservation(boundary awscloud.Boundary, dataset Dataset) awscloud.ResourceObservation {
	arn := strings.TrimSpace(dataset.ARN)
	name := strings.TrimSpace(dataset.Name)
	resourceID := datasetResourceID(dataset)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDatabrewDataset,
		Name:         name,
		Tags:         cloneStringMap(dataset.Tags),
		Attributes: map[string]any{
			"source_kind":              strings.TrimSpace(dataset.SourceKind),
			"format":                   strings.TrimSpace(dataset.Format),
			"s3_bucket":                strings.TrimSpace(dataset.S3Bucket),
			"s3_key":                   strings.TrimSpace(dataset.S3Key),
			"glue_database":            strings.TrimSpace(dataset.GlueDatabaseName),
			"glue_table":               strings.TrimSpace(dataset.GlueTableName),
			"glue_catalog_id":          strings.TrimSpace(dataset.GlueCatalogID),
			"database_connection_name": strings.TrimSpace(dataset.DatabaseConnectionName),
			"create_date":              timeOrNil(dataset.CreateDate),
			"last_modified_date":       timeOrNil(dataset.LastModifiedDate),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func recipeObservation(boundary awscloud.Boundary, recipe Recipe) awscloud.ResourceObservation {
	arn := strings.TrimSpace(recipe.ARN)
	name := strings.TrimSpace(recipe.Name)
	resourceID := recipeResourceID(recipe)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDatabrewRecipe,
		Name:         name,
		Tags:         cloneStringMap(recipe.Tags),
		Attributes: map[string]any{
			"version":            strings.TrimSpace(recipe.Version),
			"project_name":       strings.TrimSpace(recipe.ProjectName),
			"step_count":         recipe.StepCount,
			"create_date":        timeOrNil(recipe.CreateDate),
			"last_modified_date": timeOrNil(recipe.LastModifiedDate),
			"published_date":     timeOrNil(recipe.PublishedDate),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func jobObservation(boundary awscloud.Boundary, job Job) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	name := strings.TrimSpace(job.Name)
	resourceID := jobResourceID(job)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDatabrewJob,
		Name:         name,
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"type":               strings.TrimSpace(job.Type),
			"dataset_name":       strings.TrimSpace(job.DatasetName),
			"project_name":       strings.TrimSpace(job.ProjectName),
			"recipe_name":        strings.TrimSpace(job.RecipeName),
			"role_arn":           strings.TrimSpace(job.RoleARN),
			"encryption_mode":    strings.TrimSpace(job.EncryptionMode),
			"output_s3_buckets":  cloneStrings(job.OutputS3Buckets),
			"create_date":        timeOrNil(job.CreateDate),
			"last_modified_date": timeOrNil(job.LastModifiedDate),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func projectObservation(boundary awscloud.Boundary, project Project) awscloud.ResourceObservation {
	arn := strings.TrimSpace(project.ARN)
	name := strings.TrimSpace(project.Name)
	resourceID := projectResourceID(project)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDatabrewProject,
		Name:         name,
		Tags:         cloneStringMap(project.Tags),
		Attributes: map[string]any{
			"dataset_name":       strings.TrimSpace(project.DatasetName),
			"recipe_name":        strings.TrimSpace(project.RecipeName),
			"role_arn":           strings.TrimSpace(project.RoleARN),
			"create_date":        timeOrNil(project.CreateDate),
			"last_modified_date": timeOrNil(project.LastModifiedDate),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
