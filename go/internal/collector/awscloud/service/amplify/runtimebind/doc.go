// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the AWS Amplify scanner with the awsruntime
// registry through an init side effect.
//
// Importing this package once (via the awsruntime/bindings aggregate) installs
// the production Amplify scanner builder so the collector can dispatch the
// amplify service_kind. The Amplify scanner needs no redaction key because the
// SDK adapter drops every secret-bearing field at the boundary, so the
// registration leaves RequiresRedactionKey unset.
package runtimebind
