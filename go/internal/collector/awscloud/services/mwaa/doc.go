// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mwaa maps Amazon Managed Workflows for Apache Airflow (MWAA)
// environment metadata into AWS cloud collector facts.
//
// The scanner emits one reported-confidence resource per environment plus
// relationships for the S3 DAG bucket, VPC subnets, VPC security groups, the
// IAM execution role, the KMS key, and the CloudWatch Logs log groups the
// environment publishes Airflow logs to. Apache Airflow configuration option
// values, connection strings, CLI tokens, web-login tokens, REST API
// invocations, and every create/update/delete mutation stay outside this
// package contract: the scanner-owned Environment type has no field that can
// hold a configuration value or secret, and the SDK adapter read surface
// excludes the mutation and token APIs by construction.
package mwaa
