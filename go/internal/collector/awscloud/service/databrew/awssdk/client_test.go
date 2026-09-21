// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatabrew "github.com/aws/aws-sdk-go-v2/service/databrew"
	awsdatabrewtypes "github.com/aws/aws-sdk-go-v2/service/databrew/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsDatabrewMetadataOnly(t *testing.T) {
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeDatabrewAPI{
		datasetPages: []*awsdatabrew.ListDatasetsOutput{{
			Datasets: []awsdatabrewtypes.Dataset{{
				Name:        aws.String("sales"),
				ResourceArn: aws.String("arn:aws:databrew:us-east-1:123456789012:dataset/sales"),
				Source:      awsdatabrewtypes.SourceS3,
				Format:      awsdatabrewtypes.InputFormatCsv,
				CreateDate:  aws.Time(createdAt),
				Input: &awsdatabrewtypes.Input{
					S3InputDefinition: &awsdatabrewtypes.S3Location{
						Bucket: aws.String("sales-input-bucket"),
						Key:    aws.String("raw/sales/"),
					},
				},
				Tags: map[string]string{"Environment": "prod"},
			}},
		}},
		recipePages: []*awsdatabrew.ListRecipesOutput{{
			Recipes: []awsdatabrewtypes.Recipe{{
				Name:          aws.String("clean-sales"),
				ResourceArn:   aws.String("arn:aws:databrew:us-east-1:123456789012:recipe/clean-sales"),
				RecipeVersion: aws.String("1.0"),
				ProjectName:   aws.String("sales-prep"),
				Steps: []awsdatabrewtypes.RecipeStep{
					{Action: &awsdatabrewtypes.RecipeAction{Operation: aws.String("REMOVE_NULLS")}},
					{Action: &awsdatabrewtypes.RecipeAction{Operation: aws.String("UPPER_CASE")}},
				},
			}},
		}},
		jobPages: []*awsdatabrew.ListJobsOutput{{
			Jobs: []awsdatabrewtypes.Job{{
				Name:           aws.String("profile-sales"),
				ResourceArn:    aws.String("arn:aws:databrew:us-east-1:123456789012:job/profile-sales"),
				Type:           awsdatabrewtypes.JobTypeProfile,
				DatasetName:    aws.String("sales"),
				RoleArn:        aws.String("arn:aws:iam::123456789012:role/databrew-service-role"),
				EncryptionMode: awsdatabrewtypes.EncryptionModeSsekms,
				RecipeReference: &awsdatabrewtypes.RecipeReference{
					Name: aws.String("clean-sales"),
				},
				Outputs: []awsdatabrewtypes.Output{
					{Location: &awsdatabrewtypes.S3Location{Bucket: aws.String("sales-output-bucket")}},
					{Location: &awsdatabrewtypes.S3Location{Bucket: aws.String("sales-output-bucket")}},
				},
			}},
		}},
		projectPages: []*awsdatabrew.ListProjectsOutput{{
			Projects: []awsdatabrewtypes.Project{{
				Name:        aws.String("sales-prep"),
				ResourceArn: aws.String("arn:aws:databrew:us-east-1:123456789012:project/sales-prep"),
				DatasetName: aws.String("sales"),
				RecipeName:  aws.String("clean-sales"),
				RoleArn:     aws.String("arn:aws:iam::123456789012:role/databrew-service-role"),
			}},
		}},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Datasets) != 1 {
		t.Fatalf("len(Datasets) = %d, want 1", len(snapshot.Datasets))
	}
	dataset := snapshot.Datasets[0]
	if dataset.S3Bucket != "sales-input-bucket" {
		t.Fatalf("dataset S3Bucket = %q, want sales-input-bucket", dataset.S3Bucket)
	}
	if dataset.SourceKind != "S3" {
		t.Fatalf("dataset SourceKind = %q, want S3", dataset.SourceKind)
	}
	if dataset.Tags["Environment"] != "prod" {
		t.Fatalf("dataset tag Environment = %q, want prod", dataset.Tags["Environment"])
	}

	if len(snapshot.Recipes) != 1 {
		t.Fatalf("len(Recipes) = %d, want 1", len(snapshot.Recipes))
	}
	recipe := snapshot.Recipes[0]
	if recipe.StepCount != 2 {
		t.Fatalf("recipe StepCount = %d, want 2 (count only, never expressions)", recipe.StepCount)
	}

	if len(snapshot.Jobs) != 1 {
		t.Fatalf("len(Jobs) = %d, want 1", len(snapshot.Jobs))
	}
	job := snapshot.Jobs[0]
	if job.RoleARN != "arn:aws:iam::123456789012:role/databrew-service-role" {
		t.Fatalf("job RoleARN = %q, unexpected", job.RoleARN)
	}
	if len(job.OutputS3Buckets) != 1 || job.OutputS3Buckets[0] != "sales-output-bucket" {
		t.Fatalf("job OutputS3Buckets = %#v, want one de-duplicated bucket", job.OutputS3Buckets)
	}
	if job.RecipeName != "clean-sales" {
		t.Fatalf("job RecipeName = %q, want clean-sales", job.RecipeName)
	}

	if len(snapshot.Projects) != 1 {
		t.Fatalf("len(Projects) = %d, want 1", len(snapshot.Projects))
	}
	project := snapshot.Projects[0]
	if project.RecipeName != "clean-sales" || project.DatasetName != "sales" {
		t.Fatalf("project bindings = %q/%q, want sales/clean-sales", project.DatasetName, project.RecipeName)
	}
}

func TestClientPaginatesEveryList(t *testing.T) {
	api := &fakeDatabrewAPI{
		datasetPages: []*awsdatabrew.ListDatasetsOutput{
			{Datasets: []awsdatabrewtypes.Dataset{{Name: aws.String("a")}}, NextToken: aws.String("p2")},
			{Datasets: []awsdatabrewtypes.Dataset{{Name: aws.String("b")}}},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Datasets) != 2 {
		t.Fatalf("len(Datasets) = %d, want 2 across two pages", len(snapshot.Datasets))
	}
	if api.datasetCall != 2 {
		t.Fatalf("ListDatasets called %d times, want 2", api.datasetCall)
	}
}

func TestClientHandlesEmptyAccountCleanly(t *testing.T) {
	client := &Client{client: &fakeDatabrewAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Datasets)+len(snapshot.Recipes)+len(snapshot.Jobs)+len(snapshot.Projects) != 0 {
		t.Fatalf("expected an empty snapshot, got %#v", snapshot)
	}
}

type fakeDatabrewAPI struct {
	datasetPages []*awsdatabrew.ListDatasetsOutput
	datasetCall  int
	recipePages  []*awsdatabrew.ListRecipesOutput
	recipeCall   int
	jobPages     []*awsdatabrew.ListJobsOutput
	jobCall      int
	projectPages []*awsdatabrew.ListProjectsOutput
	projectCall  int
}

func (f *fakeDatabrewAPI) ListDatasets(
	_ context.Context,
	_ *awsdatabrew.ListDatasetsInput,
	_ ...func(*awsdatabrew.Options),
) (*awsdatabrew.ListDatasetsOutput, error) {
	if f.datasetCall >= len(f.datasetPages) {
		return &awsdatabrew.ListDatasetsOutput{}, nil
	}
	page := f.datasetPages[f.datasetCall]
	f.datasetCall++
	return page, nil
}

func (f *fakeDatabrewAPI) ListRecipes(
	_ context.Context,
	_ *awsdatabrew.ListRecipesInput,
	_ ...func(*awsdatabrew.Options),
) (*awsdatabrew.ListRecipesOutput, error) {
	if f.recipeCall >= len(f.recipePages) {
		return &awsdatabrew.ListRecipesOutput{}, nil
	}
	page := f.recipePages[f.recipeCall]
	f.recipeCall++
	return page, nil
}

func (f *fakeDatabrewAPI) ListJobs(
	_ context.Context,
	_ *awsdatabrew.ListJobsInput,
	_ ...func(*awsdatabrew.Options),
) (*awsdatabrew.ListJobsOutput, error) {
	if f.jobCall >= len(f.jobPages) {
		return &awsdatabrew.ListJobsOutput{}, nil
	}
	page := f.jobPages[f.jobCall]
	f.jobCall++
	return page, nil
}

func (f *fakeDatabrewAPI) ListProjects(
	_ context.Context,
	_ *awsdatabrew.ListProjectsInput,
	_ ...func(*awsdatabrew.Options),
) (*awsdatabrew.ListProjectsOutput, error) {
	if f.projectCall >= len(f.projectPages) {
		return &awsdatabrew.ListProjectsOutput{}, nil
	}
	page := f.projectPages[f.projectCall]
	f.projectCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDatabrew,
	}
}
