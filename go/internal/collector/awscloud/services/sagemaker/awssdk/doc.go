// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 SageMaker control-plane calls into
// scanner-owned metadata.
//
// The adapter only calls List and Describe reads plus ListTags. It never calls
// any mutation operation and never calls InvokeEndpoint or InvokeEndpointAsync;
// those inference operations live in the separate sagemakerruntime module,
// which this package does not import. The adapter never copies hyperparameter
// values, training/processing/transform data references, notebook
// lifecycle-config script bodies, container environment maps, or pipeline
// definition bodies into scanner-owned types. The reflection gate in
// exclusion_test.go fails the build if a forbidden method ever reaches the
// adapter-local apiClient interface.
package awssdk
