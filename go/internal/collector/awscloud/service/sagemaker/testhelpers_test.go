// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// fakeClient is a scanner-owned Client double. Each slice seeds one List
// response so scanner tests can exercise resource and relationship emission
// without the AWS SDK.
type fakeClient struct {
	notebooks           []NotebookInstance
	models              []Model
	endpoints           []Endpoint
	endpointConfigs     []EndpointConfig
	trainingJobs        []TrainingJob
	processingJobs      []ProcessingJob
	transformJobs       []TransformJob
	tuningJobs          []HyperParameterTuningJob
	projects            []Project
	pipelines           []Pipeline
	featureGroups       []FeatureGroup
	domains             []Domain
	userProfiles        []UserProfile
	apps                []App
	inferenceComponents []InferenceComponent
}

func (c *fakeClient) ListNotebookInstances(context.Context) ([]NotebookInstance, error) {
	return c.notebooks, nil
}
func (c *fakeClient) ListModels(context.Context) ([]Model, error)       { return c.models, nil }
func (c *fakeClient) ListEndpoints(context.Context) ([]Endpoint, error) { return c.endpoints, nil }
func (c *fakeClient) ListEndpointConfigs(context.Context) ([]EndpointConfig, error) {
	return c.endpointConfigs, nil
}

func (c *fakeClient) ListTrainingJobs(context.Context) ([]TrainingJob, error) {
	return c.trainingJobs, nil
}

func (c *fakeClient) ListProcessingJobs(context.Context) ([]ProcessingJob, error) {
	return c.processingJobs, nil
}

func (c *fakeClient) ListTransformJobs(context.Context) ([]TransformJob, error) {
	return c.transformJobs, nil
}

func (c *fakeClient) ListHyperParameterTuningJobs(context.Context) ([]HyperParameterTuningJob, error) {
	return c.tuningJobs, nil
}
func (c *fakeClient) ListProjects(context.Context) ([]Project, error)   { return c.projects, nil }
func (c *fakeClient) ListPipelines(context.Context) ([]Pipeline, error) { return c.pipelines, nil }
func (c *fakeClient) ListFeatureGroups(context.Context) ([]FeatureGroup, error) {
	return c.featureGroups, nil
}
func (c *fakeClient) ListDomains(context.Context) ([]Domain, error) { return c.domains, nil }
func (c *fakeClient) ListUserProfiles(context.Context) ([]UserProfile, error) {
	return c.userProfiles, nil
}
func (c *fakeClient) ListApps(context.Context) ([]App, error) { return c.apps, nil }
func (c *fakeClient) ListInferenceComponents(context.Context) ([]InferenceComponent, error) {
	return c.inferenceComponents, nil
}

// flatten renders a payload into a deterministic string so tests can assert a
// forbidden value never appears anywhere in an emitted fact.
func flatten(value any) string {
	var b strings.Builder
	flattenInto(&b, value)
	return b.String()
}

func flattenInto(b *strings.Builder, value any) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteString(key)
			b.WriteByte('=')
			flattenInto(b, typed[key])
			b.WriteByte(';')
		}
	case map[string]string:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(b, "%s=%s;", key, typed[key])
		}
	case []any:
		for _, item := range typed {
			flattenInto(b, item)
			b.WriteByte(',')
		}
	case []string:
		for _, item := range typed {
			b.WriteString(item)
			b.WriteByte(',')
		}
	case []map[string]any:
		for _, item := range typed {
			flattenInto(b, item)
			b.WriteByte(',')
		}
	default:
		fmt.Fprintf(b, "%v", typed)
	}
}

var _ Client = (*fakeClient)(nil)
