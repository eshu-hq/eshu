// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenancestore

// ControlSchemaSQL exposes controlSchemaSQL so external tests can assert on
// the runtime_ingester_control DDL text without duplicating it.
const ControlSchemaSQL = controlSchemaSQL

// RepositoryReindexSchemaSQL exposes repositoryReindexSchemaSQL so external
// tests can check it against the embedded migration.
const RepositoryReindexSchemaSQL = repositoryReindexSchemaSQL

// RequestRepositoryReindexQuery exposes requestRepositoryReindexQuery so
// external tests can assert on its shape.
const RequestRepositoryReindexQuery = requestRepositoryReindexQuery
