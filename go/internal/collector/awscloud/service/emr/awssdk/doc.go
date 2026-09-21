// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 EMR and EMR Serverless calls into
// scanner-owned EMR metadata.
//
// On the EMR side the adapter calls ListClusters (bounded to running and
// recently terminated states within a recent CreatedAfter window),
// DescribeCluster, ListInstanceGroups or ListInstanceFleets by collection type,
// ListSecurityConfigurations (name and creation time only), ListStudios,
// DescribeStudio, and ListStudioSessionMappings. On the EMR Serverless side it
// calls ListApplications and GetApplication.
//
// The adapter must not call any mutation API, must not call ListSteps,
// DescribeStep, or ListBootstrapActions (step command lines and bootstrap
// script bodies), must not call DescribeSecurityConfiguration (the encryption
// and authentication policy body), and must not call any EMR Serverless job-run
// reader (GetJobRun, ListJobRuns, ListJobRunAttempts) because those carry
// SparkSubmit entry-point arguments. The package tests enforce these
// boundaries by reflecting over the SDK interfaces the adapter accepts.
package awssdk
