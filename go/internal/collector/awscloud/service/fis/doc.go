// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fis maps AWS Fault Injection Service (FIS) experiment-template
// control-plane metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for FIS experiment templates
// plus relationships for the execution IAM role, the explicit resource targets
// (EC2 instance, ECS cluster, RDS DB instance/cluster), the logging
// destinations (CloudWatch Logs log group, S3 bucket), and the CloudWatch alarm
// stop conditions. Action parameter values, target filter values, experiment
// run results, and any start/stop or mutation API stay outside this package
// contract: the scanner is metadata-only.
package fis
