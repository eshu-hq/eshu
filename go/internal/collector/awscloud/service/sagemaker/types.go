// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"context"
	"time"
)

// Client is the SageMaker control-plane read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned
// metadata records. The interface deliberately exposes no inference call
// (InvokeEndpoint / InvokeEndpointAsync) and no mutation operation, so the
// scanner cannot run a model or change SageMaker state through this contract.
type Client interface {
	ListNotebookInstances(context.Context) ([]NotebookInstance, error)
	ListModels(context.Context) ([]Model, error)
	ListEndpoints(context.Context) ([]Endpoint, error)
	ListEndpointConfigs(context.Context) ([]EndpointConfig, error)
	ListTrainingJobs(context.Context) ([]TrainingJob, error)
	ListProcessingJobs(context.Context) ([]ProcessingJob, error)
	ListTransformJobs(context.Context) ([]TransformJob, error)
	ListHyperParameterTuningJobs(context.Context) ([]HyperParameterTuningJob, error)
	ListProjects(context.Context) ([]Project, error)
	ListPipelines(context.Context) ([]Pipeline, error)
	ListFeatureGroups(context.Context) ([]FeatureGroup, error)
	ListDomains(context.Context) ([]Domain, error)
	ListUserProfiles(context.Context) ([]UserProfile, error)
	ListApps(context.Context) ([]App, error)
	ListInferenceComponents(context.Context) ([]InferenceComponent, error)
}

// NotebookInstance is the scanner-owned representation of one SageMaker notebook
// instance. The scanner persists placement and lifecycle-config name metadata
// but never persists the lifecycle-config script body.
type NotebookInstance struct {
	ARN              string
	Name             string
	Status           string
	InstanceType     string
	SubnetID         string
	SecurityGroupIDs []string
	// DirectInternetAccess reports the AWS-reported direct-internet-access
	// setting ("Enabled"/"Disabled") as inventory metadata.
	DirectInternetAccess string
	// LifecycleConfigName names the attached lifecycle config. The script body
	// the config carries is intentionally outside the contract.
	LifecycleConfigName string
	PlatformIdentifier  string
	CreationTime        time.Time
	LastModifiedTime    time.Time
	Tags                map[string]string
}

// Model is the scanner-owned representation of one SageMaker model. Container
// environment maps are intentionally excluded because they can hold
// secret-like values.
type Model struct {
	ARN             string
	Name            string
	ExecutionRole   string
	VPCSubnetIDs    []string
	NetworkIsolated bool
	// Containers carries inference container image and artifact references in
	// pipeline order. Container Environment maps are never captured.
	Containers   []ModelContainer
	CreationTime time.Time
	Tags         map[string]string
}

// ModelContainer is one inference container reference for a SageMaker model. It
// carries the image URI and S3 model-artifact location only.
type ModelContainer struct {
	Image        string
	ModelDataURL string
}

// Endpoint is the scanner-owned representation of one SageMaker endpoint.
type Endpoint struct {
	ARN              string
	Name             string
	Status           string
	EndpointConfig   string
	CreationTime     time.Time
	LastModifiedTime time.Time
	Tags             map[string]string
}

// EndpointConfig is the scanner-owned representation of one SageMaker endpoint
// configuration. ProductionVariants carry the model dependency only.
type EndpointConfig struct {
	ARN          string
	Name         string
	KMSKeyID     string
	ModelNames   []string
	CreationTime time.Time
	Tags         map[string]string
}

// TrainingJob is the scanner-owned representation of one SageMaker training job.
// Hyperparameter values and training data references are never persisted.
type TrainingJob struct {
	ARN              string
	Name             string
	Status           string
	SecondaryStatus  string
	ExecutionRole    string
	CreationTime     time.Time
	LastModifiedTime time.Time
	TrainingEndTime  time.Time
	Tags             map[string]string
}

// ProcessingJob is the scanner-owned representation of one SageMaker processing
// job. Processing input/output data contents are never persisted.
type ProcessingJob struct {
	ARN              string
	Name             string
	Status           string
	CreationTime     time.Time
	LastModifiedTime time.Time
	ProcessingEnd    time.Time
	Tags             map[string]string
}

// TransformJob is the scanner-owned representation of one SageMaker batch
// transform job. Transform input/output data contents are never persisted.
type TransformJob struct {
	ARN              string
	Name             string
	Status           string
	CreationTime     time.Time
	LastModifiedTime time.Time
	TransformEnd     time.Time
	Tags             map[string]string
}

// HyperParameterTuningJob is the scanner-owned representation of one SageMaker
// hyperparameter tuning job. Tuned hyperparameter values are never persisted;
// only the bounded strategy label and status are kept.
type HyperParameterTuningJob struct {
	ARN              string
	Name             string
	Status           string
	Strategy         string
	CreationTime     time.Time
	LastModifiedTime time.Time
	Tags             map[string]string
}

// Project is the scanner-owned representation of one SageMaker project.
type Project struct {
	ARN          string
	ID           string
	Name         string
	Status       string
	CreationTime time.Time
	Tags         map[string]string
}

// Pipeline is the scanner-owned representation of one SageMaker pipeline. The
// pipeline definition body is never read or persisted.
type Pipeline struct {
	ARN              string
	Name             string
	DisplayName      string
	CreationTime     time.Time
	LastModifiedTime time.Time
	LastExecution    time.Time
	Tags             map[string]string
}

// FeatureGroup is the scanner-owned representation of one SageMaker feature
// group. Feature record contents are never read or persisted.
type FeatureGroup struct {
	ARN          string
	Name         string
	Status       string
	OfflineStore string
	CreationTime time.Time
	Tags         map[string]string
}
