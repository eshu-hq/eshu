// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 FIS client into the metadata-only
// AWS Fault Injection Service scanner interface.
//
// The adapter uses ListExperimentTemplates, GetExperimentTemplate, and
// ListTagsForResource to read FIS experiment-template control-plane metadata and
// resource tags. It intentionally excludes StartExperiment, StopExperiment,
// every experiment run read (GetExperiment, ListExperiments,
// ListExperimentResolvedTargets), and all Create/Update/Delete mutation APIs,
// so the adapter cannot launch a fault-injection experiment, read experiment
// run results, or mutate FIS state.
package awssdk
