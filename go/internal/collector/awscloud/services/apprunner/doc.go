// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package apprunner maps AWS App Runner observations into AWS cloud fact
// envelopes.
//
// The package owns scanner-level App Runner fact selection for services,
// connections, automatic scaling configurations, observability configurations,
// VPC connectors, VPC ingress connections, and their relationships. It is
// metadata-only and never persists source repository credentials or runtime
// environment-variable values: only environment-variable names are kept, and
// secret references are carried as Secrets Manager / SSM ARN reference edges.
// AWS SDK pagination, credentials, persistence, graph projection, and
// reducer-owned correlation live outside this package.
package apprunner
