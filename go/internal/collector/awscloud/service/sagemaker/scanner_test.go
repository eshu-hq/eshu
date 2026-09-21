// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsResourcesAndRelationshipsMetadataOnly(t *testing.T) {
	envelopes := scanFixture(t, richClient())

	// One resource per in-scope SageMaker resource type.
	for _, resourceType := range []string{
		awscloud.ResourceTypeSageMakerNotebookInstance,
		awscloud.ResourceTypeSageMakerModel,
		awscloud.ResourceTypeSageMakerEndpoint,
		awscloud.ResourceTypeSageMakerEndpointConfig,
		awscloud.ResourceTypeSageMakerTrainingJob,
		awscloud.ResourceTypeSageMakerProcessingJob,
		awscloud.ResourceTypeSageMakerTransformJob,
		awscloud.ResourceTypeSageMakerHyperParameterTuningJob,
		awscloud.ResourceTypeSageMakerProject,
		awscloud.ResourceTypeSageMakerPipeline,
		awscloud.ResourceTypeSageMakerFeatureGroup,
		awscloud.ResourceTypeSageMakerDomain,
		awscloud.ResourceTypeSageMakerUserProfile,
		awscloud.ResourceTypeSageMakerApp,
		awscloud.ResourceTypeSageMakerInferenceComponent,
	} {
		resourceByType(t, envelopes, resourceType)
	}

	// All required relationships are emitted.
	for _, relationshipType := range []string{
		awscloud.RelationshipSageMakerModelUsesS3Artifact,
		awscloud.RelationshipSageMakerModelUsesContainerImage,
		awscloud.RelationshipSageMakerModelUsesIAMRole,
		awscloud.RelationshipSageMakerEndpointUsesEndpointConfig,
		awscloud.RelationshipSageMakerEndpointConfigUsesModel,
		awscloud.RelationshipSageMakerTrainingJobUsesIAMRole,
		awscloud.RelationshipSageMakerNotebookInstanceUsesSubnet,
		awscloud.RelationshipSageMakerDomainUsesVPC,
		awscloud.RelationshipSageMakerUserProfileInDomain,
	} {
		assertRelationship(t, envelopes, relationshipType)
	}
}

func TestScannerNeverPersistsSensitivePayloads(t *testing.T) {
	envelopes := scanFixture(t, richClient())

	// Hyperparameter values must never appear anywhere in the emitted facts.
	for _, envelope := range envelopes {
		flat := strings.ToLower(flatten(envelope.Payload))
		for _, banned := range []string{
			"secret-hyperparameter",     // a hyperparameter value
			"s3://training/input/data",  // training input data reference
			"s3://training/output/data", // training output data reference
			"lifecycle-script-body",     // notebook lifecycle config script body
			"pipeline-definition-body",  // pipeline definition JSON body
			"container-env-secret",      // container environment value
		} {
			if strings.Contains(flat, banned) {
				t.Fatalf("emitted fact %q contains forbidden payload %q: %s", envelope.FactKind, banned, flat)
			}
		}
	}
}

func TestScannerTrainingJobOmitsHyperParameterAttribute(t *testing.T) {
	envelopes := scanFixture(t, richClient())
	job := resourceByType(t, envelopes, awscloud.ResourceTypeSageMakerTrainingJob)
	attributes := attributesOf(t, job)
	for key := range attributes {
		if strings.Contains(strings.ToLower(key), "hyperparameter") {
			t.Fatalf("training job attributes carry hyperparameter key %q; values may be secret-like", key)
		}
	}
	if got, want := attributes["execution_role_arn"], "arn:aws:iam::123456789012:role/train"; got != want {
		t.Fatalf("execution_role_arn = %#v, want %q", got, want)
	}
}

func TestScannerModelRelationshipsTargetTypes(t *testing.T) {
	envelopes := scanFixture(t, richClient())
	artifact := relationshipByType(t, envelopes, awscloud.RelationshipSageMakerModelUsesS3Artifact)
	if got, want := artifact.Payload["target_resource_id"], "arn:aws:s3:::artifacts"; got != want {
		t.Fatalf("model artifact target = %#v, want %q", got, want)
	}
	if got, want := artifact.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("model artifact target_type = %#v, want %q", got, want)
	}
	image := relationshipByType(t, envelopes, awscloud.RelationshipSageMakerModelUsesContainerImage)
	if got, want := image.Payload["target_resource_id"], "123456789012.dkr.ecr.us-east-1.amazonaws.com/infer:latest"; got != want {
		t.Fatalf("model image target = %#v, want %q", got, want)
	}
	role := relationshipByType(t, envelopes, awscloud.RelationshipSageMakerModelUsesIAMRole)
	if got, want := role.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("model role target_type = %#v, want %q", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR
	if _, err := (Scanner{Client: richClient()}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerHandlesEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: &fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() emitted %d envelopes for empty account, want 0", len(envelopes))
	}
}

func scanFixture(t *testing.T, client Client) []facts.Envelope {
	t.Helper()
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	return envelopes
}

func richClient() *fakeClient {
	return &fakeClient{
		notebooks: []NotebookInstance{{
			ARN:                 "arn:aws:sagemaker:us-east-1:123456789012:notebook-instance/nb",
			Name:                "nb",
			Status:              "InService",
			SubnetID:            "subnet-aaa",
			LifecycleConfigName: "nb-lifecycle",
			Tags:                map[string]string{"Team": "ml"},
		}},
		models: []Model{{
			ARN:           "arn:aws:sagemaker:us-east-1:123456789012:model/m",
			Name:          "m",
			ExecutionRole: "arn:aws:iam::123456789012:role/model",
			Containers: []ModelContainer{{
				Image:        "123456789012.dkr.ecr.us-east-1.amazonaws.com/infer:latest",
				ModelDataURL: "s3://artifacts/model.tar.gz",
			}},
		}},
		endpoints: []Endpoint{{
			ARN:            "arn:aws:sagemaker:us-east-1:123456789012:endpoint/e",
			Name:           "e",
			Status:         "InService",
			EndpointConfig: "ec",
		}},
		endpointConfigs: []EndpointConfig{{
			ARN:        "arn:aws:sagemaker:us-east-1:123456789012:endpoint-config/ec",
			Name:       "ec",
			ModelNames: []string{"m"},
		}},
		trainingJobs: []TrainingJob{{
			ARN:           "arn:aws:sagemaker:us-east-1:123456789012:training-job/tj",
			Name:          "tj",
			Status:        "Completed",
			ExecutionRole: "arn:aws:iam::123456789012:role/train",
		}},
		processingJobs: []ProcessingJob{{
			ARN:    "arn:aws:sagemaker:us-east-1:123456789012:processing-job/pj",
			Name:   "pj",
			Status: "Completed",
		}},
		transformJobs: []TransformJob{{
			ARN:    "arn:aws:sagemaker:us-east-1:123456789012:transform-job/xj",
			Name:   "xj",
			Status: "Completed",
		}},
		tuningJobs: []HyperParameterTuningJob{{
			ARN:      "arn:aws:sagemaker:us-east-1:123456789012:hyper-parameter-tuning-job/hpo",
			Name:     "hpo",
			Status:   "Completed",
			Strategy: "Bayesian",
		}},
		projects: []Project{{
			ARN:    "arn:aws:sagemaker:us-east-1:123456789012:project/p",
			ID:     "p-1",
			Name:   "p",
			Status: "CreateCompleted",
		}},
		pipelines: []Pipeline{{
			ARN:  "arn:aws:sagemaker:us-east-1:123456789012:pipeline/pl",
			Name: "pl",
		}},
		featureGroups: []FeatureGroup{{
			ARN:    "arn:aws:sagemaker:us-east-1:123456789012:feature-group/fg",
			Name:   "fg",
			Status: "Created",
		}},
		domains: []Domain{{
			ARN:    "arn:aws:sagemaker:us-east-1:123456789012:domain/d-1",
			ID:     "d-1",
			Name:   "studio",
			Status: "InService",
			VPCID:  "vpc-aaa",
		}},
		userProfiles: []UserProfile{{
			Name:     "alice",
			DomainID: "d-1",
			Status:   "InService",
		}},
		apps: []App{{
			ARN:         "arn:aws:sagemaker:us-east-1:123456789012:app/d-1/alice/JupyterServer/default",
			Name:        "default",
			Type:        "JupyterServer",
			DomainID:    "d-1",
			UserProfile: "alice",
			Status:      "InService",
		}},
		inferenceComponents: []InferenceComponent{{
			ARN:          "arn:aws:sagemaker:us-east-1:123456789012:inference-component/ic",
			Name:         "ic",
			Status:       "InService",
			EndpointName: "e",
			EndpointARN:  "arn:aws:sagemaker:us-east-1:123456789012:endpoint/e",
		}},
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSageMaker,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:sagemaker:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	relationshipByType(t, envelopes, relationshipType)
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
