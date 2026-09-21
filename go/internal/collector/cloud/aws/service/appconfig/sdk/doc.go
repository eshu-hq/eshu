// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 AppConfig client into the
// metadata-only AppConfig scanner interface.
//
// The adapter uses ListApplications, ListEnvironments, ListConfigurationProfiles,
// and ListDeploymentStrategies to read AppConfig control-plane identity and
// deployment metadata. It intentionally excludes GetConfiguration,
// GetHostedConfigurationVersion, the entire appconfigdata module (which it never
// imports), every deployment Start/Stop API, and all Create/Update/Delete
// mutation APIs, so the adapter cannot read configuration content or mutate
// AppConfig state.
package awssdk
