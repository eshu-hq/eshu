// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 MWAA client into the
// metadata-only MWAA scanner interface.
//
// The adapter uses ListEnvironments and GetEnvironment only. It intentionally
// excludes CreateEnvironment, UpdateEnvironment, DeleteEnvironment,
// CreateCliToken, CreateWebLoginToken, InvokeRestApi, PublishMetrics,
// TagResource, and UntagResource, so the adapter can never mutate an
// environment, mint an Apache Airflow CLI or web-login token, or invoke the
// Airflow REST API. The mapper never reads AirflowConfigurationOptions (the
// Apache Airflow configuration option values), the Celery executor queue ARN,
// the database VPC endpoint service, or the webserver URL, so configuration
// values, internal queue identities, and webserver endpoints never leave the
// adapter.
package awssdk
