// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package appsync emits metadata-only AWS AppSync facts for GraphQL APIs, data
// sources, resolvers, pipeline functions, schema metadata, and API key
// lifecycle summaries.
//
// The package owns scanner-side resource and relationship fact shaping for API
// identities, authentication configuration, data-source backing-resource
// targets, resolver and function data-source bindings, and Cognito user pool
// and OIDC issuer authentication evidence. It deliberately excludes the schema
// definition language (SDL) body, resolver request and response mapping
// templates, pipeline function code bodies, API key values, key identifiers,
// and key descriptions, because those are high-IP or credential surfaces.
// AppSync's API key Id is the bearer key itself; the SDK adapter drops it and
// retains only an API-level count with lifecycle timestamps. Reducers, not this
// package, own any workload, repository, environment, or deployable-unit
// inference.
package appsync
