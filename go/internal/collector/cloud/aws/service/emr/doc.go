// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package emr maps Amazon EMR metadata into AWS cloud collector facts.
//
// One service kind covers EMR on EC2 clusters, EMR Serverless applications,
// and EMR Studio. The package owns scanner-level fact selection for clusters
// (running and recently terminated), uniform instance groups, instance fleets,
// security configurations (name only), Serverless applications, Studios, and
// Studio session mappings, plus the network, IAM, security configuration, and
// KMS relationship edges those resources report.
//
// The scanner is metadata-only. It never invokes job, step, or lifecycle
// mutation APIs (RunJobFlow, TerminateJobFlows, AddJobFlowSteps, CancelSteps,
// ModifyInstanceGroups, ModifyInstanceFleet, StartJobRun, CancelJobRun,
// Create/Delete/Start/Stop Application, Create/Delete/Update Studio or
// StudioSessionMapping), and never persists step command lines, bootstrap
// action script bodies, security configuration policy bodies, or Serverless
// job-run SparkSubmit entry-point arguments. Those payloads have no field on
// the scanner-owned types, so a leak path does not exist.
//
// The EMR cluster and EMR Serverless APIs do not report a VPC id directly, so
// the cluster-to-VPC and application-to-VPC joins are derived from subnet
// membership downstream; this scanner emits the subnet edges. EMR Studio
// reports its VPC id, so the studio-to-VPC edge is emitted directly.
package emr
