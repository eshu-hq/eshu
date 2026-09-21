// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind registers the AWS Amplify scanner with the runtime
// registry through an init side effect.
//
// Importing this package once (via the runtime/bindings aggregate) installs
// the production Amplify scanner builder so the collector can dispatch the
// amplify service_kind. The Amplify scanner needs no redaction key because the
// SDK adapter drops every secret-bearing field at the boundary, so the
// registration leaves RequiresRedactionKey unset.
package bind
