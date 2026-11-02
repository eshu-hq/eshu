// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package config holds the admin provider-config wire DTOs
// (Detail, RevisionItem, WriteResult, ConnectionTestResult, and the
// Create/Update/RevertRequest store requests) as a neutral leaf so both
// the provider-config handler family and query/testutil can name them
// without an import cycle (#7715). The family package
// internal/query/admin/provider/config forwards these types through
// aliases and owns the ports; this leaf owns only the data shapes.
package config
