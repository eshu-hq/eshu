// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Glue DataBrew client into the
// metadata-only DataBrew scanner interface.
//
// The adapter uses ListDatasets, ListRecipes, ListJobs, and ListProjects to
// read DataBrew control-plane metadata. It intentionally excludes every
// Describe* detail read (which would expose recipe step expressions or dataset
// sample data) and every Create/Update/Delete/Start/Stop/Publish/Send mutation
// API, so the adapter cannot read recipe step expressions, custom SQL query
// strings, sample data, or mutate DataBrew state. It records only the recipe
// step count, never the step expressions or their transformation parameters.
package awssdk
