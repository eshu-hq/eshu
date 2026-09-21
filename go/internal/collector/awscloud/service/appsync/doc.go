// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package appsync emits metadata-only AWS AppSync facts for GraphQL APIs, data
// sources, resolvers, pipeline functions, schema metadata, and API key
// metadata.
//
// The package owns scanner-side resource and relationship fact shaping for API
// identities, authentication configuration, data-source backing-resource
// targets, resolver and function data-source bindings, and Cognito user pool
// and OIDC issuer authentication evidence. It deliberately excludes the schema
// definition language (SDL) body, resolver request and response mapping
// templates, pipeline function code bodies, and API key values, because those
// are high-IP or credential surfaces. The scanner-owned types have no field for
// any of them, so the leak paths do not exist. Reducers, not this package, own
// any workload, repository, environment, or deployable-unit inference.
package appsync
